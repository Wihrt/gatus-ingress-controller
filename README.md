# gatus-ingress-controller

[![Build](https://github.com/Wihrt/gatus-ingress-controller/actions/workflows/main.yml/badge.svg)](https://github.com/Wihrt/gatus-ingress-controller/actions/workflows/main.yml)
[![Coverage](https://codecov.io/gh/Wihrt/gatus-ingress-controller/graph/badge.svg)](https://codecov.io/gh/Wihrt/gatus-ingress-controller)

A Kubernetes controller that automatically generates [Gatus](https://github.com/TwiN/gatus) monitoring endpoints from Ingress and Gateway API HTTPRoute resources.

When an Ingress is created or updated, the controller creates a `GatusEndpoint` custom resource for each hostname. All endpoints are then aggregated into a single ConfigMap that Gatus reads to configure its monitoring.

## How it works

```
ConfigMap "gatus-config"
  ├── config.yaml              (user-managed: DB, web, etc.)
  ├── endpoints.yaml           (GatusEndpointReconciler)
  ├── external-endpoints.yaml  (GatusExternalEndpointReconciler)
  ├── alerting.yaml            (GatusAlertReconciler)
  ├── announcements.yaml       (GatusAnnouncementReconciler)
  └── maintenance.yaml         (GatusMaintenanceReconciler)
          │
          └── mounted at /config in Gatus pod → Gatus merges all files
```

Each reconciler watches its own CRD cluster-wide, builds the corresponding Gatus config section, and writes it as a dedicated key inside the shared ConfigMap. The ConfigMap must be pre-created — the controller never creates or deletes it.

Ingresses and HTTPRoutes are also watched: for each hostname the controller automatically creates a `GatusEndpoint` CR (or removes it when `gatus.io/enabled: "false"`), which is then picked up by the `GatusEndpointReconciler`.

## Installation

### 1. Create the shared ConfigMap

The controller **appends** to an existing ConfigMap — it never creates one from scratch. You must pre-create the ConfigMap with your main Gatus configuration (storage, alerting providers, web settings, etc.):

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
| `ingressClass` | `traefik` | Only watch Ingresses with this class |
| `targetNamespace` | `gatus` | Namespace where the ConfigMap is written |
| `configMapName` | `gatus-config` | Name of the ConfigMap to append endpoints to (must pre-exist) |
| `gatewayApi.enabled` | `false` | Enable HTTPRoute controller (requires Gateway API CRDs) |
| `gatewayApi.gatewayNames` | `""` | Comma-separated Gateway names to filter HTTPRoutes (empty = all) |
| `image.repository` | `ghcr.io/wihrt/gatus-ingress-controller` | Controller image |
| `image.tag` | `latest` | Image tag |
| `replicaCount` | `1` | Number of replicas |

## Usage

### Automatic monitoring from an Ingress

By default, any Ingress matching the configured ingress class gets a Gatus `HTTP 200` check automatically:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
spec:
  ingressClassName: traefik
  rules:
    - host: my-app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-app
                port:
                  number: 80
```

This creates a `GatusEndpoint` monitoring `https://my-app.example.com` with condition `[STATUS] == 200`.

### Annotations

Control monitoring behaviour with annotations on your Ingress (or HTTPRoute):

| Annotation | Values | Description |
|---|---|---|
| `gatus.io/enabled` | `"true"` / `"false"` | Disable monitoring for this resource (default: enabled) |
| `gatus.io/group` | any string | Group name shown on the Gatus dashboard (default: `external`) |
| `gatus.io/alerts` | comma-separated names | `GatusAlert` resources to attach |

```yaml
metadata:
  annotations:
    gatus.io/enabled: "true"
    gatus.io/group: "production"
    gatus.io/alerts: "slack-alert, pagerduty-alert"
```

### Disable monitoring for a specific Ingress

```yaml
metadata:
  annotations:
    gatus.io/enabled: "false"
```

### Configure an alert provider (GatusAlert)

A `GatusAlert` serves two purposes: it configures the alert provider in Gatus (webhook URL, etc.) **and** defines the default thresholds used when the alert is referenced from an endpoint. One CR per provider type — if two CRs share the same type, the first (alphabetically) is used and a warning is logged.

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusAlert
metadata:
  name: slack-alert
  namespace: default
spec:
  type: slack
  webhookUrl: "https://hooks.slack.com/services/..."
  failureThreshold: 3      # alert after 3 consecutive failures
  successThreshold: 2      # resolve after 2 consecutive successes
  sendOnResolved: true
  description: "Endpoint is down"
```

Then reference it from an Ingress:

```yaml
metadata:
  annotations:
    gatus.io/alerts: "slack-alert"
```

Supported alert types: `slack`, `discord`, `teams`, `teams-workflows`, `pagerduty`, `opsgenie`, `telegram`, `email`, `ntfy`, and [many more](https://github.com/TwiN/gatus#alerting).

> **Note:** Provider-specific configuration not covered by `GatusAlert` (e.g. email host/port, PagerDuty routing keys) should be added manually to `config.yaml`.

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

### Define a global maintenance window (GatusMaintenance)

Gatus suppresses all alerts during the configured maintenance window. Only **one** `GatusMaintenance` CR is active at a time (the first alphabetically); extras are ignored with a warning.

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusMaintenance
metadata:
  name: nightly-maintenance
  namespace: default
spec:
  enabled: true
  start: "23:00"                # HH:MM (24-hour)
  duration: "1h"
  timezone: "Europe/Amsterdam"
  every:                        # omit to apply every day
    - Monday
    - Thursday
```

### Create a monitoring endpoint manually (GatusEndpoint)

For resources that are not backed by an Ingress (e.g. an external API, a TCP port, a DNS record):

```yaml
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusEndpoint
metadata:
  name: external-api
  namespace: default
spec:
  name: "External API"
  group: "external"
  url: "https://api.example.com/health"
  interval: "60s"
  conditions:
    - "[STATUS] == 200"
    - "[RESPONSE_TIME] < 500"
    - "[BODY] == pat:*healthy*"
  alerts:
    - name: slack-alert
```

> **Override auto-generated endpoints:** if a `GatusEndpoint` CR is created manually (without the `gatus.io/managed-by` label) and shares the same name or Gatus display name as an auto-generated CR, the **manual CR always wins**. The auto-generated CR is not updated and its config is excluded from `endpoints.yaml`.

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

### Gateway API (HTTPRoute)

Enable HTTPRoute support when your cluster has the Gateway API CRDs installed:

```bash
helm upgrade gatus-ingress-controller oci://ghcr.io/wihrt/charts/gatus-ingress-controller \
  --set gatewayApi.enabled=true \
  --set gatewayApi.gatewayNames="my-gateway"
```

HTTPRoutes support the same `gatus.io/*` annotations as Ingresses.

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
