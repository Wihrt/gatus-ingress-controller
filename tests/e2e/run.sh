#!/usr/bin/env bash
# E2E test script for the gatus-ingress-controller.
# Assumes a kubeconfig is already configured (e.g., k3s).
# Usage: ./tests/e2e/run.sh <image-tag>
set -euo pipefail

IMAGE_TAG="${1:-ci-test}"
IMAGE="${IMAGE_REGISTRY:-ghcr.io/wihrt/gatus-ingress-controller}:${IMAGE_TAG}"
NAMESPACE="gatus-system"
TARGET_NAMESPACE="gatus"
INGRESS_CLASS="traefik"
TIMEOUT=90s

echo "==> E2E test: gatus-ingress-controller"
echo "    Image:            ${IMAGE}"
echo "    Namespace:        ${NAMESPACE}"
echo "    Target namespace: ${TARGET_NAMESPACE}"
echo "    Ingress class:    ${INGRESS_CLASS}"

# ── 1. Namespaces ──────────────────────────────────────────────────────────────
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace "${TARGET_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# ── 2. CRDs ────────────────────────────────────────────────────────────────────
echo "==> Applying CRDs..."
kubectl apply -f charts/gatus-ingress-controller/templates/crd-gatusalert.yaml
kubectl apply -f charts/gatus-ingress-controller/templates/crd-gatusendpoint.yaml
kubectl apply -f charts/gatus-ingress-controller/templates/crd-gatusexternalendpoint.yaml

# ── 3. RBAC ────────────────────────────────────────────────────────────────────
echo "==> Applying RBAC..."
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: gatus-ingress-controller
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: gatus-ingress-controller
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["monitoring.gatus.io"]
    resources: ["gatusalerts", "gatusendpoints", "gatusexternalendpoints"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["monitoring.gatus.io"]
    resources: ["gatusalerts/status", "gatusendpoints/status", "gatusexternalendpoints/status"]
    verbs: ["get", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: gatus-ingress-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: gatus-ingress-controller
subjects:
  - kind: ServiceAccount
    name: gatus-ingress-controller
    namespace: ${NAMESPACE}
EOF

# ── 4. Controller deployment ───────────────────────────────────────────────────
echo "==> Deploying controller (image: ${IMAGE})..."
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gatus-ingress-controller
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: gatus-ingress-controller
  template:
    metadata:
      labels:
        app: gatus-ingress-controller
    spec:
      serviceAccountName: gatus-ingress-controller
      containers:
        - name: gatus-ingress-controller
          image: ${IMAGE}
          imagePullPolicy: Never
          env:
            - name: INGRESS_CLASS
              value: "${INGRESS_CLASS}"
            - name: TARGET_NAMESPACE
              value: "${TARGET_NAMESPACE}"
          ports:
            - name: health
              containerPort: 8081
          livenessProbe:
            httpGet:
              path: /healthz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 5
          readinessProbe:
            httpGet:
              path: /readyz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 5
EOF

echo "==> Waiting for controller to be ready..."
kubectl rollout status deployment/gatus-ingress-controller -n "${NAMESPACE}" --timeout="${TIMEOUT}"

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

echo "==> E2E tests PASSED."
