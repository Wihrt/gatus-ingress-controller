#!/usr/bin/env bash
# Cluster bootstrap for E2E tests.
# Installs cert-manager and the gatus-ingress-controller via Helm,
# then waits for CRDs and the controller deployment to be ready.
# Usage: bash tests/e2e/setup.sh <image-tag>
set -euo pipefail

IMAGE_TAG="${1:-ci-test}"
IMAGE_REPOSITORY="${IMAGE_REGISTRY:-ghcr.io/wihrt/gatus-ingress-controller}"
NAMESPACE="gatus-system"
TARGET_NAMESPACE="gatus"
TIMEOUT="90s"
RELEASE_NAME="gatus-ingress-controller"
# renovate: datasource=helm depName=cert-manager registryUrl=https://charts.jetstack.io
CERT_MANAGER_VERSION="v1.19.4"

echo "==> E2E setup: gatus-ingress-controller"
echo "    Image:            ${IMAGE_REPOSITORY}:${IMAGE_TAG}"
echo "    Namespace:        ${NAMESPACE}"
echo "    Target namespace: ${TARGET_NAMESPACE}"

# ── 1. Target namespace and pre-existing resources ────────────────────────────
kubectl create namespace "${TARGET_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic gatus-secrets -n "${TARGET_NAMESPACE}" \
  --from-literal=endpoints.yaml="placeholder" \
  --from-literal=external-endpoints.yaml="placeholder" \
  --dry-run=client -o yaml | kubectl apply -f -

# ── 2. Deploy cert-manager ────────────────────────────────────────────────────
echo "==> Installing cert-manager (${CERT_MANAGER_VERSION}) via Helm..."
helm repo add jetstack https://charts.jetstack.io --force-update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version "${CERT_MANAGER_VERSION}" \
  --set crds.enabled=true \
  --wait \
  --timeout "${TIMEOUT}"

# ── 3. Deploy controller via Helm ─────────────────────────────────────────────
# Apply CRDs explicitly — helm upgrade does not update resources in crds/ directory
echo "==> Applying CRDs..."
kubectl apply -f charts/gatus-ingress-controller/crds/

echo "==> Installing controller via Helm (image: ${IMAGE_REPOSITORY}:${IMAGE_TAG})..."
helm upgrade --install "${RELEASE_NAME}" ./charts/gatus-ingress-controller \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=Never \
  --set targetNamespace="${TARGET_NAMESPACE}" \
  --wait \
  --timeout "${TIMEOUT}"

# ── 4. Wait for CRDs to be Established ───────────────────────────────────────
echo "==> Waiting for CRDs to be Established..."
kubectl wait --for=condition=Established --timeout="${TIMEOUT}" crd/gatusendpoints.monitoring.gatus.io
kubectl wait --for=condition=Established --timeout="${TIMEOUT}" crd/gatusexternalendpoints.monitoring.gatus.io

# ── 5. Wait for controller to be ready ────────────────────────────────────────
echo "==> Waiting for controller to be ready..."
kubectl rollout status deployment/"${RELEASE_NAME}" -n "${NAMESPACE}" --timeout="${TIMEOUT}"

sleep 10

echo "==> Cluster setup complete. Ready for E2E tests."
