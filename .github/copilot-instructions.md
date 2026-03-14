# Copilot Instructions

## Project Overview

A Kubernetes controller that manages [Gatus](https://github.com/TwiN/gatus) monitoring endpoints via custom resources, automatically aggregating them into a shared Secret that Gatus reads for its configuration.

## Commands

Tools are managed via [mise](https://mise.jdx.dev/) (`mise.toml`) and tasks via [Task](https://taskfile.dev/) (`Taskfile.yml`). Run `mise install` to install all required tools.

```bash
task build        # go build ./...
task test         # go test ./... -v
task fmt          # go fmt ./...
task vet          # go vet ./...
task docker-build # Build container image (IMAGE_REGISTRY/IMAGE_REPO:IMAGE_TAG)
task e2e          # Run e2e tests — requires a configured KUBECONFIG (e.g. k3s). Usage: task e2e TAG=ci-test
```

Run a single test:
```bash
go test ./internal/controller/... -run TestGatusEndpointReconciler_DefaultCondition -v
```

Validate GitHub Actions locally with [act](https://github.com/nektos/act) (requires Docker and `gh` CLI authenticated):
```bash
task act:pr                   # go + docker jobs of pr.yml (e2e skipped — needs real k3s)
task act:main                 # go job of main.yml (docker job pushes to registry, skipped)
task act:tag TAG=v1.2.3       # full tag workflow_dispatch; docker job needs existing SHA image in registry
task act:tag                  # uses v0.0.0-test as default tag
```

## Architecture

**Data flow**: `GatusEndpoint` CR → aggregated into Secret (`gatus-secrets/endpoints.yaml`) → Gatus reads config.

**Two reconcilers** registered in `cmd/main.go`:
- `GatusEndpointReconciler` — aggregates all `GatusEndpoint` CRs into a Secret; injects default condition `[STATUS] == 200` when `spec.conditions` is empty
- `GatusExternalEndpointReconciler` — handles externally-pushed status endpoints

**One webhook** registered in `cmd/main.go`:
- `GatusEndpointValidator` — validates `GatusEndpoint` condition syntax on CREATE/UPDATE

**Custom resources** defined in `api/v1alpha1/`:
- `GatusEndpoint` — HTTP/DNS/SSH endpoint monitoring spec with inline alerts
- `GatusExternalEndpoint` — externally-pushed status endpoints with inline alerts

**Secret keys** managed by the controller: `endpoints.yaml`, `external-endpoints.yaml`.

## Key Conventions

### Runtime configuration (env vars)
| Variable | Default | Purpose |
|---|---|---|
| `TARGET_NAMESPACE` | `gatus` | Namespace where the Secret is written |
| `SECRET_NAME` | `gatus-secrets` | Target Secret name (must pre-exist) |

### Error handling
Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`.

### Logging
Use `log.FromContext(ctx)` — never initialize a new logger directly in reconcilers.

### Tests
Unit tests use `controller-runtime`'s `fake.Client` (no envtest/cluster required). Test scheme setup follows the `newTestScheme(t *testing.T)` helper pattern in `helpers_test.go`.

### Default conditions
When `GatusEndpoint.spec.conditions` is empty, the reconciler injects `[STATUS] == 200` in the generated YAML output (the CR itself is not modified).

### Deduplication
When two `GatusEndpoint` CRs share the same `spec.name`, the first alphabetically (by namespace/name) wins.

### Inline alerts
Alerts are defined directly on GatusEndpoint/GatusExternalEndpoint with `type` and optional `providerOverride` fields, matching Gatus config format.
