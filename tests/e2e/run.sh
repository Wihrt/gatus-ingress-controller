#!/usr/bin/env bash
# E2E test script for the gatus-ingress-controller.
# Assumes a kubeconfig is already configured (e.g., k3s).
# Usage: ./tests/e2e/run.sh <image-tag>
set -euo pipefail

IMAGE_TAG="${1:-ci-test}"
IMAGE_REPOSITORY="${IMAGE_REGISTRY:-ghcr.io/wihrt/gatus-ingress-controller}"
NAMESPACE="gatus-system"
TARGET_NAMESPACE="gatus"
INGRESS_CLASS="traefik"
TIMEOUT=90s
RELEASE_NAME="gatus-ingress-controller"

echo "==> E2E test: gatus-ingress-controller"
echo "    Image:            ${IMAGE_REPOSITORY}:${IMAGE_TAG}"
echo "    Namespace:        ${NAMESPACE}"
echo "    Target namespace: ${TARGET_NAMESPACE}"
echo "    Ingress class:    ${INGRESS_CLASS}"

# ── 1. Target namespace ────────────────────────────────────────────────────────
kubectl create namespace "${TARGET_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# ── 2. Deploy controller via Helm ──────────────────────────────────────────────
echo "==> Installing controller via Helm (image: ${IMAGE_REPOSITORY}:${IMAGE_TAG})..."
helm install "${RELEASE_NAME}" ./charts/gatus-ingress-controller \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=Never \
  --set targetNamespace="${TARGET_NAMESPACE}" \
  --set ingressClass="${INGRESS_CLASS}"

# ── 3. Wait for CRDs to be Established ───────────────────────────────────────
echo "==> Waiting for CRDs to be Established..."
kubectl wait --for=condition=Established --timeout="${TIMEOUT}" crd/gatusalerts.monitoring.gatus.io
kubectl wait --for=condition=Established --timeout="${TIMEOUT}" crd/gatusendpoints.monitoring.gatus.io
kubectl wait --for=condition=Established --timeout="${TIMEOUT}" crd/gatusexternalendpoints.monitoring.gatus.io

# ── 4. Wait for controller to be ready ────────────────────────────────────────
echo "==> Waiting for controller to be ready..."
kubectl rollout status deployment/"${RELEASE_NAME}" -n "${NAMESPACE}" --timeout="${TIMEOUT}"

# ── 5. Create test Ingress ─────────────────────────────────────────────────────
echo "==> Creating test Ingress..."
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: test-app
  namespace: default
spec:
  ingressClassName: ${INGRESS_CLASS}
  rules:
    - host: test-app.example.com
EOF

# ── 6. Wait for GatusEndpoint to be created ────────────────────────────────────
echo "==> Waiting for GatusEndpoint to be created..."
ENDPOINT_NAME="test-app-test-app-example-com"
for i in $(seq 1 30); do
  if kubectl get gatusendpoint "${ENDPOINT_NAME}" -n default &>/dev/null; then
    echo "    GatusEndpoint '${ENDPOINT_NAME}' found after ${i}s."
    break
  fi
  if [ "${i}" -eq 30 ]; then
    echo "ERROR: GatusEndpoint '${ENDPOINT_NAME}' not found after 30s"
    kubectl get events -n "${NAMESPACE}" --sort-by='.lastTimestamp' | tail -20
    exit 1
  fi
  sleep 1
done

# ── 7. Assert GatusEndpoint content ───────────────────────────────────────────
echo "==> Asserting GatusEndpoint content..."
ACTUAL_URL=$(kubectl get gatusendpoint "${ENDPOINT_NAME}" -n default -o jsonpath='{.spec.url}')
EXPECTED_URL="https://test-app.example.com"
if [ "${ACTUAL_URL}" != "${EXPECTED_URL}" ]; then
  echo "ERROR: expected URL '${EXPECTED_URL}', got '${ACTUAL_URL}'"
  kubectl get gatusendpoint "${ENDPOINT_NAME}" -n default -o yaml
  exit 1
fi
echo "    URL OK: ${ACTUAL_URL}"

ACTUAL_GROUP=$(kubectl get gatusendpoint "${ENDPOINT_NAME}" -n default -o jsonpath='{.spec.group}')
EXPECTED_GROUP="external"
if [ "${ACTUAL_GROUP}" != "${EXPECTED_GROUP}" ]; then
  echo "ERROR: expected group '${EXPECTED_GROUP}', got '${ACTUAL_GROUP}'"
  exit 1
fi
echo "    Group OK: ${ACTUAL_GROUP}"

# ── 8. Assert ConfigMap is written ────────────────────────────────────────────
echo "==> Waiting for ConfigMap '${TARGET_NAMESPACE}/gatus-config' to be created..."
for i in $(seq 1 30); do
  if kubectl get configmap gatus-config -n "${TARGET_NAMESPACE}" &>/dev/null; then
    echo "    ConfigMap found after ${i}s."
    break
  fi
  if [ "${i}" -eq 30 ]; then
    echo "ERROR: ConfigMap 'gatus-config' not found in namespace '${TARGET_NAMESPACE}' after 30s"
    exit 1
  fi
  sleep 1
done

# ── 9. User-managed GatusEndpoint must not be overwritten ─────────────────────
echo "==> Testing that user-managed GatusEndpoint is not overwritten by auto-generation..."

OVERRIDE_INGRESS="test-override"
OVERRIDE_ENDPOINT_NAME="test-override-test-override-example-com"
CUSTOM_URL="https://custom-override.example.com"
CUSTOM_GROUP="user-managed-group"

# Create a manual GatusEndpoint (no ownerReferences).
kubectl apply -f - <<EOF
apiVersion: monitoring.gatus.io/v1alpha1
kind: GatusEndpoint
metadata:
  name: ${OVERRIDE_ENDPOINT_NAME}
  namespace: default
spec:
  name: "Override Test"
  group: "${CUSTOM_GROUP}"
  url: "${CUSTOM_URL}"
  conditions:
    - "[STATUS] == 200"
EOF

# Create an Ingress with the same host — controller would normally generate the same endpoint name.
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ${OVERRIDE_INGRESS}
  namespace: default
  annotations:
    kubernetes.io/ingress.class: "${INGRESS_CLASS}"
    gatus.io/enabled: "true"
    gatus.io/group: "auto-group"
spec:
  ingressClassName: "${INGRESS_CLASS}"
  rules:
    - host: test-override.example.com
EOF

# Give controller time to reconcile.
sleep 10

# Assert the user-managed spec is preserved.
ACTUAL_URL=$(kubectl get gatusendpoint "${OVERRIDE_ENDPOINT_NAME}" -n default -o jsonpath='{.spec.url}')
if [ "${ACTUAL_URL}" != "${CUSTOM_URL}" ]; then
  echo "ERROR: User-managed GatusEndpoint URL was overwritten! got '${ACTUAL_URL}', want '${CUSTOM_URL}'"
  kubectl get gatusendpoint "${OVERRIDE_ENDPOINT_NAME}" -n default -o yaml
  exit 1
fi
echo "    URL preserved: ${ACTUAL_URL}"

ACTUAL_GROUP=$(kubectl get gatusendpoint "${OVERRIDE_ENDPOINT_NAME}" -n default -o jsonpath='{.spec.group}')
if [ "${ACTUAL_GROUP}" != "${CUSTOM_GROUP}" ]; then
  echo "ERROR: User-managed GatusEndpoint group was overwritten! got '${ACTUAL_GROUP}', want '${CUSTOM_GROUP}'"
  exit 1
fi
echo "    Group preserved: ${ACTUAL_GROUP}"

# Cleanup override test resources.
kubectl delete ingress "${OVERRIDE_INGRESS}" -n default --ignore-not-found
kubectl delete gatusendpoint "${OVERRIDE_ENDPOINT_NAME}" -n default --ignore-not-found

echo "==> E2E tests PASSED."
