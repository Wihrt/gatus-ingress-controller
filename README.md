# gatus-controller

[![Build](https://github.com/Wihrt/gatus-controller/actions/workflows/main.yml/badge.svg)](https://github.com/Wihrt/gatus-controller/actions/workflows/main.yml)
[![Coverage](https://codecov.io/gh/Wihrt/gatus-controller/graph/badge.svg)](https://codecov.io/gh/Wihrt/gatus-controller)

A Kubernetes controller that manages [Gatus](https://github.com/TwiN/gatus) monitoring endpoints via custom resources, automatically aggregating them into a shared Secret that Gatus reads for its configuration.

## How it works

```
Secret "gatus-secrets"
  ├── endpoints.yaml           (GatusEndpointReconciler)
  └── external-endpoints.yaml  (GatusExternalEndpointReconciler)
        │
        └── mounted in Gatus pod → Gatus merges all files
```

Each reconciler watches its own CRD cluster-wide, builds the corresponding Gatus config section, and writes it as a dedicated key inside the shared Secret. The Secret must be pre-created — the controller never creates or deletes it.

## Custom Resource Definitions

| CRD | Description |
|---|---|
| `GatusEndpoint` | HTTP/DNS/SSH endpoint monitoring configuration with inline alerts |
| `GatusExternalEndpoint` | Externally-pushed status endpoints with inline alerts |

## Installation

### 1. Create the shared Secret

The controller **appends** to an existing Secret — it never creates it from scratch:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gatus-secrets
  namespace: gatus
type: Opaque
```

### 2. Deploy Gatus

Point Gatus at the shared Secret and enable directory-based config reading with `GATUS_CONFIG_PATH=/config`:

```bash
helm install gatus oci://ghcr.io/twin/helm-charts/gatus \
  --namespace gatus \
  --create-namespace \
  --set env.GATUS_CONFIG_PATH=/config
```

> Gatus reads **all `.yaml` files** in `/config` when `GATUS_CONFIG_PATH` points to a directory. The controller writes `endpoints.yaml` and `external-endpoints.yaml` alongside your `config.yaml`.

### 3. Deploy the controller

```bash
helm install gatus-controller oci://ghcr.io/wihrt/charts/gatus-controller \
  --namespace gatus-system \
  --create-namespace
```

### Helm values

| Value | Default | Description |
|---|---|---|
| `targetNamespace` | `gatus` | Namespace where the Secret is written |
| `secretName` | `gatus-secrets` | Name of the Secret to write endpoint data to (must pre-exist) |
| `webhook.enabled` | `true` | Enable validating webhooks for CRDs (requires cert-manager) |
| `image.repository` | `ghcr.io/wihrt/gatus-controller` | Controller image |
| `image.tag` | `latest` | Image tag |
| `replicaCount` | `1` | Number of replicas |

> **Prerequisites:** When `webhook.enabled` is `true`, [cert-manager](https://cert-manager.io/) must be installed in the cluster to provision TLS certificates for the webhook server.

## Usage

### Create a monitoring endpoint (GatusEndpoint)

Define a `GatusEndpoint` CR for any service you want Gatus to monitor. If no conditions are specified, the controller defaults to `[STATUS] == 200`:

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
    - type: slack
      enabled: true
      failureThreshold: 3
      successThreshold: 2
      sendOnResolved: true
      description: "My App is down"
      providerOverride:
        webhook-url: "https://hooks.slack.com/services/..."
```

Alerts are configured inline with the `type` field specifying the alert provider. Supported alert types: `slack`, `discord`, `teams`, `teams-workflows`, `pagerduty`, `opsgenie`, `telegram`, `email`, `ntfy`, and [many more](https://github.com/TwiN/gatus#alerting).

The `providerOverride` field allows overriding specific provider configuration fields per-endpoint, matching the Gatus `provider-override` configuration.

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
    - type: slack
      failureThreshold: 5
      sendOnResolved: true
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
task act:pr                  # PR workflow: go → docker → e2e (e2e skipped locally, needs real k3s)
task act:main                # Main workflow: go job (docker job pushes to registry, skipped)
task act:tag TAG=v1.2.3      # Tag workflow: full dispatch (docker job needs existing SHA image)
```
