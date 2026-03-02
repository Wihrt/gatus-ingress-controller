# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Kubernetes controller that automatically generates [Gatus](https://github.com/TwiN/gatus) monitoring endpoints from Ingress and Gateway API HTTPRoute resources. It watches cluster resources and aggregates monitoring configurations into a shared ConfigMap.

## Commands

Tools managed via [mise](https://mise.jdx.dev/) (`mise.toml`), tasks via [Task](https://taskfile.dev/) (`Taskfile.yml`).

```bash
mise install              # Install all tools (go, helm, task, act, hadolint, pre-commit)
task build                # go build ./...
task test                 # go test ./... -v
task fmt                  # go fmt ./...
task vet                  # go vet ./...
task docker-build         # Build container image
task e2e TAG=ci-test      # E2E tests (requires k3s + KUBECONFIG)
task hooks:install        # Install pre-commit hooks
task act:pr               # Simulate PR workflow locally with act
task act:main             # Simulate main workflow locally
task act:tag TAG=v1.2.3   # Simulate tag workflow locally
```

Run a single test:
```bash
go test ./internal/controller/... -run TestSanitizeHostname -v
```

Pre-commit hooks run `go vet`, `go build`, `go test`, `hadolint`, and `helm lint` automatically on commit.

## Architecture

**Data flow**: Ingress/HTTPRoute → `GatusEndpoint` CR → aggregated into ConfigMap (`gatus-config/endpoints.yaml`) → Gatus reads config.

**Reconcilers** (registered in `cmd/main.go`):
- `IngressReconciler` — watches Ingresses, creates/updates/deletes `GatusEndpoint` CRs
- `HTTPRouteReconciler` — opt-in Gateway API support (`GATEWAY_API_ENABLED=true`)
- `GatusEndpointReconciler` — aggregates all `GatusEndpoint` CRs into ConfigMap
- `GatusExternalEndpointReconciler` — handles externally-pushed status endpoints
- `GatusAlertReconciler` — validates alert provider configs, aggregates into ConfigMap
- `GatusAnnouncementReconciler` — aggregates status page announcements
- `GatusMaintenanceReconciler` — aggregates global maintenance windows

**CRDs** (`api/v1alpha1/`): GatusEndpoint, GatusAlert, GatusExternalEndpoint, GatusAnnouncement, GatusMaintenance.

**ConfigMap keys** managed by the controller: `endpoints.yaml`, `external-endpoints.yaml`, `alerting.yaml`, `announcements.yaml`, `maintenance.yaml`. User-managed `config.yaml` is preserved.

## Key Conventions

- **Annotations** on Ingress/HTTPRoute: `gatus.io/enabled`, `gatus.io/group`, `gatus.io/alerts`
- **Labels** on managed resources: `gatus.io/managed-by: gatus-ingress-controller`, `gatus.io/ingress: <name>`
- **Hostname sanitization**: dots→dashes, `*`→`wildcard` (e.g. `my-ingress-wildcard-example-com`)
- **Error handling**: wrap with `fmt.Errorf("failed to X: %w", err)`
- **Logging**: use `log.FromContext(ctx)`, never initialize loggers in reconcilers
- **Tests**: use `fake.Client` (no envtest), each test file has `newTestScheme(t)` helper
- **Manual CR override**: a GatusEndpoint without `gatus.io/managed-by` label takes priority over auto-generated ones

## Runtime Env Vars

| Variable | Default | Purpose |
|---|---|---|
| `INGRESS_CLASS` | `traefik` | Only reconcile Ingresses with this class |
| `TARGET_NAMESPACE` | `gatus` | Namespace where ConfigMap is written |
| `CONFIG_MAP_NAME` | `gatus-config` | Target ConfigMap name |
| `GATEWAY_API_ENABLED` | `false` | Enable HTTPRoute controller |
| `GATEWAY_NAMES` | _(all)_ | Filter HTTPRoutes by parent gateway names |

## Docker

Multi-stage build: `golang:1.24-alpine` → `gcr.io/distroless/static:nonroot`. Binary at `/manager`, runs as UID 65532.

## Helm Chart

Located in `charts/gatus-ingress-controller/`. CRDs in `charts/gatus-ingress-controller/crds/`. Version bumped automatically by the tag workflow.

## Release Process

Manual `workflow_dispatch` on `tag.yml` with version input (e.g. `v1.2.3`):
1. Create & push git tag
2. Retag Docker image from SHA to version tag
3. Bump Chart.yaml, package & push Helm chart to OCI registry
4. Create GitHub Release with changelog and Helm artifact
