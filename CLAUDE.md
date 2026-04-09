# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Kubernetes controller that manages [Gatus](https://github.com/TwiN/gatus) monitoring endpoints via custom resources, aggregating configurations into a shared Secret that Gatus reads.

## Commands

Tools managed via [mise](https://mise.jdx.dev/) (`mise.toml`), tasks via [Task](https://taskfile.dev/) (`Taskfile.yml`).

```bash
mise install                   # Install all tools (go, helm, task, act, chainsaw, hadolint, pre-commit)
task build                     # go build ./...
task test                      # go test ./... -v
task fmt                       # go fmt ./...
task vet                       # go vet ./...
task docker-build              # Build container image
task e2e TAG=ci-test           # Run Chainsaw E2E tests (requires k3s + KUBECONFIG)
task act:pr                    # Simulate PR workflow locally with act
task act:main                  # Simulate main workflow locally
task act:tag TAG=v1.2.3        # Simulate tag workflow locally
```

Run a single test:
```bash
go test ./internal/controller/... -run TestGatusEndpointReconciler_DefaultCondition -v
```

Pre-commit hooks run `go vet`, `go build`, `go test`, `hadolint`, and `helm lint` automatically on commit.

## Architecture

**Data flow**: `GatusEndpoint` CR → aggregated into Secret (`gatus-secrets/endpoints.yaml`) → Gatus reads config.

**Reconcilers** (registered in `cmd/main.go`):
- `GatusEndpointReconciler` — aggregates all `GatusEndpoint` CRs into Secret; injects default condition `[STATUS] == 200` when `spec.conditions` is empty
- `GatusExternalEndpointReconciler` — handles externally-pushed status endpoints

**CRDs** (`api/v1alpha1/`): GatusEndpoint, GatusExternalEndpoint.

**Secret keys** managed by the controller: `endpoints.yaml`, `external-endpoints.yaml`.

## Key Conventions

- **Error handling**: wrap with `fmt.Errorf("failed to X: %w", err)`
- **Logging**: use `log.FromContext(ctx)`, never initialize loggers in reconcilers
- **Tests**: use `fake.Client` (no envtest), shared `newTestScheme(t)` helper in `helpers_test.go`
- **Default conditions**: when `GatusEndpoint.spec.conditions` is empty, the reconciler injects `[STATUS] == 200` in the generated YAML output (the CR itself is not modified)
- **Deduplication**: when two `GatusEndpoint` CRs share the same `spec.name`, the first alphabetically (by namespace/name) wins
- **Inline alerts**: alerts are defined directly on GatusEndpoint/GatusExternalEndpoint with `type` and optional `providerOverride` fields, matching Gatus config format

## Runtime Env Vars

| Variable | Default | Purpose |
|---|---|---|
| `TARGET_NAMESPACE` | `gatus` | Namespace where Secret is written |
| `SECRET_NAME` | `gatus-secrets` | Target Secret name |

## Docker

Multi-stage build: `golang:1.24-alpine` → `gcr.io/distroless/static:nonroot`. Binary at `/manager`, runs as UID 65532.

## Helm Chart

Located in `charts/gatus-controller/`. CRDs in `charts/gatus-controller/crds/`. Version bumped automatically by the tag workflow.

## Release Process

Manual `workflow_dispatch` on `tag.yml` with version input (e.g. `v1.2.3`):
1. Create & push git tag
2. Retag Docker image from SHA to version tag
3. Bump Chart.yaml, package & push Helm chart to OCI registry
4. Create GitHub Release with changelog and Helm artifact
