# gatus-ingress-controller

[![Build](https://github.com/Wihrt/gatus-ingress-controller/actions/workflows/main.yml/badge.svg)](https://github.com/Wihrt/gatus-ingress-controller/actions/workflows/main.yml)
[![Coverage](https://codecov.io/gh/Wihrt/gatus-ingress-controller/graph/badge.svg)](https://codecov.io/gh/Wihrt/gatus-ingress-controller)

A Kubernetes controller that manages [Gatus](https://github.com/TwiN/gatus) monitoring endpoints via custom resources, automatically aggregating them into a shared ConfigMap and Secret that Gatus reads for its configuration.

## How it works

```
ConfigMap "gatus-config"                Secret "gatus-secrets"
  ├── config.yaml   (user-managed)        ├── endpoints.yaml           (GatusEndpointReconciler)
  └── announcements.yaml                  ├── external-endpoints.yaml  (GatusExternalEndpointReconciler)
        (GatusAnnouncementReconciler)     └── alerting.yaml            (GatusAlertingConfigReconciler
          │                                                              + GatusAlertReconciler)
          └── mounted in Gatus pod → Gatus merges all files
```

Each reconciler watches its own CRD cluster-wide, builds the corresponding Gatus config section, and writes it as a dedicated key inside the shared ConfigMap or Secret. Non-sensitive data (announcements) goes into the ConfigMap; sensitive data (endpoints, alerting) goes into the Secret. Both must be pre-created — the controller never creates or deletes them.

## Installation

### 1. Create the shared ConfigMap and Secret

The controller **appends** to existing resources — it never creates them from scratch. You must pre-create the ConfigMap with your main Gatus configuration and the Secret for sensitive data:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: gatus-config
  namespace: gatus
data:
  config.yaml: |
    web:
      port: 8080
    storage:
      type: sqlite
      path: /data/gatus.db
---
apiVersion: v1
kind: Secret
metadata:
  name: gatus-secrets
  namespace: gatus
type: Opaque
```

### 2. Deploy Gatus

Point Gatus at the shared ConfigMap using the `externalConfigMap` value and enable directory-based config reading with `GATUS_CONFIG_PATH=/config`:

```bash
helm install gatus oci://ghcr.io/twin/helm-charts/gatus \
  --namespace gatus \
  --create-namespace \
  --set externalConfigMap=gatus-config \
  --set env.GATUS_CONFIG_PATH=/config
```

> Gatus reads **all `.yaml` files** in `/config` when `GATUS_CONFIG_PATH` points to a directory. The controller writes `endpoints.yaml`, `external-endpoints.yaml`, `alerting.yaml`, `announcements.yaml`, and `maintenance.yaml` alongside your `config.yaml`.

### 3. Deploy the controller

```bash
helm install gatus-ingress-controller oci://ghcr.io/wihrt/charts/gatus-ingress-controller \
  --namespace gatus-system \
  --create-namespace
```

### Helm values

| Value | Default | Description |
|---|---|---|
| `targetNamespace` | `gatus` | Namespace where the ConfigMap and Secret are written |
| `configMapName` | `gatus-config` | Name of the ConfigMap to append non-sensitive data to (must pre-exist) |
| `secretName` | `gatus-secrets` | Name of the Secret to append sensitive data to (must pre-exist) |
| `webhook.enabled` | `true` | Enable validating webhooks for CRDs (requires cert-manager) |
| `image.repository` | `ghcr.io/wihrt/gatus-ingress-controller` | Controller image |
| `image.tag` | `latest` | Image tag |
| `replicaCount` | `1` | Number of replicas |

> **Prerequisites:** When `webhook.enabled` is `true`, [cert-manager](https://cert-manager.io/) must be installed in the cluster to provision TLS certificates for the webhook server.

## Usage

### Create a monitoring endpoint (GatusEndpoint)

Define a `GatusEndpoint` CR for any service you want Gatus to monitor. If no conditions are specified, the controller defaults to `[STATUS] == 200`:

A `GatusAlertingConfig` configures a single alert provider in Gatus. Each provider type (e.g. slack, discord) can only have one `GatusAlertingConfig` cluster-wide:

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusAlertingConfig
metadata:
  name: slack-config
  namespace: default
spec:
  type: slack
  config:
    webhook-url: "https://hooks.slack.com/services/..."
```

For sensitive values, use `configSecretRef` to reference a Kubernetes Secret (e.g. managed by External Secrets Operator):

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusAlertingConfig
metadata:
  name: slack-config
  namespace: default
spec:
  type: slack
  configSecretRef:
    name: slack-secret   # Secret data keys are merged on top of spec.config
```

A `GatusAlert` defines the default alert thresholds and references a `GatusAlertingConfig`:

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusAlert
metadata:
  name: slack-alert
  namespace: default
spec:
  alertingConfigRef: slack-config  # references the GatusAlertingConfig above
  failureThreshold: 3
  successThreshold: 2
  sendOnResolved: true
  description: "Endpoint is down"
```

Reference it from a `GatusEndpoint`:

```yaml
spec:
  alerts:
    - name: slack-alert
```

Supported alert types: `slack`, `discord`, `teams`, `teams-workflows`, `pagerduty`, `opsgenie`, `telegram`, `email`, `ntfy`, and [many more](https://github.com/TwiN/gatus#alerting).

### Add a status page announcement (GatusAnnouncement)

Announcements are displayed on the Gatus status page. All `GatusAnnouncement` CRs are aggregated and sorted newest-first:

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusAnnouncement
metadata:
  name: scheduled-maintenance
  namespace: default
spec:
  timestamp: "2025-11-07T22:00:00Z"
  type: warning          # outage | warning | information | operational | none
  message: "Scheduled database maintenance window tonight from 22:00 to 23:00 UTC."
  archived: false        # true moves it to the "Past Announcements" section
```

### Create a monitoring endpoint manually (GatusEndpoint)

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusEndpoint
metadata:
  name: my-app
  namespace: default
spec:
  name: "My App"
  group: "production"
  url: "https://my-app.example.com/health"
  interval: "60s"
  conditions:
    - "[STATUS] == 200"
    - "[RESPONSE_TIME] < 500"
    - "[BODY] == pat:*healthy*"
  alerts:
    - name: slack-alert
```

If `conditions` is omitted, the controller injects `[STATUS] == 200` automatically.

### Configure an alert provider (GatusAlert)

### External endpoint (status pushed by your app)

For services that push their own status to Gatus rather than being polled:

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusExternalEndpoint
metadata:
  name: my-worker
  namespace: default
spec:
  name: "Background Worker"
  group: "workers"
  token: "my-secret-token"
  heartbeat:
    interval: "30m"   # alert if no update received within 30 minutes
  alerts:
    - name: slack-alert
```

## Development

### Prerequisites

Install [mise](https://mise.jdx.dev/), then run:

```bash
mise install
```

This installs all required tools (`go`, `helm`, `task`, `act`, `hadolint`, `pre-commit`) and automatically installs the pre-commit hooks via the `mise.toml` `enter` hook.

### Tasks

All commands are managed via [Task](https://taskfile.dev/):

```bash
task build            # go build ./...
task test             # go test ./... -v
task fmt              # go fmt ./...
task vet              # go vet ./...
task docker-build     # build container image
task e2e TAG=ci-test  # e2e tests — requires a configured KUBECONFIG (e.g. k3s)
```

### Pre-commit hooks

Hooks run automatically on `git commit`:

| Hook | Trigger |
|---|---|
| `go vet` / `go build` / `go test` | any staged `.go` file |
| `hadolint` | `Dockerfile` changes |
| `helm lint` | any change under `charts/` |
| end-of-file fixer / trailing whitespace | all files |

To run all hooks manually:

```bash
pre-commit run --all-files
```

### Validate GitHub Actions locally

Using [act](https://github.com/nektos/act) — requires Docker and `gh` CLI authenticated:

```bash
task act:pr                  # PR workflow: go + docker jobs (e2e skipped, needs real k3s)
task act:main                # Main workflow: go job (docker job pushes to registry, skipped)
task act:tag TAG=v1.2.3      # Tag workflow: full dispatch (docker job needs existing SHA image)
```
