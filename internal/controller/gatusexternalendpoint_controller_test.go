package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"gopkg.in/yaml.v3"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func newExtEndpointReconciler(fakeClient client.Client) *GatusExternalEndpointReconciler {
	return &GatusExternalEndpointReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		ConfigMapName:   "gatus-config",
	}
}

func extEndpointConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "gatus-config", Namespace: "gatus"},
		Data:       map[string]string{"config.yaml": "web:\n  port: 8080\n"},
	}
}

func reconcileExtEndpoint(t *testing.T, r *GatusExternalEndpointReconciler, name, namespace string) ctrl.Result {
	t.Helper()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	if err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}
	return result
}

func getExternalEndpointsYAML(t *testing.T, fakeClient client.Client) map[string]interface{} {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm); err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}
	raw, ok := cm.Data["external-endpoints.yaml"]
	if !ok {
		t.Fatal("external-endpoints.yaml key not found in ConfigMap")
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("external-endpoints.yaml is not valid YAML: %v\ncontent:\n%s", err, raw)
	}
	return out
}

// TestGatusExternalEndpointReconciler_WritesExternalEndpointsYAML verifies a single
// GatusExternalEndpoint CR is written to external-endpoints.yaml with correct fields.
func TestGatusExternalEndpointReconciler_WritesExternalEndpointsYAML(t *testing.T) {
	s := newTestScheme(t)
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "my-service", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:    "My External Service",
			Enabled: true,
			Group:   "production",
			Token:   "super-secret-token",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointConfigMap(), ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	reconcileExtEndpoint(t, r, "my-service", "default")

	out := getExternalEndpointsYAML(t, fakeClient)
	endpoints, ok := out["external-endpoints"].([]interface{})
	if !ok || len(endpoints) == 0 {
		t.Fatalf("expected non-empty external-endpoints list, got: %v", out["external-endpoints"])
	}
	entry := endpoints[0].(map[string]interface{})
	if entry["name"] != "My External Service" {
		t.Errorf("name = %v, want 'My External Service'", entry["name"])
	}
	if entry["group"] != "production" {
		t.Errorf("group = %v, want 'production'", entry["group"])
	}
	if entry["token"] != "super-secret-token" {
		t.Errorf("token = %v, want 'super-secret-token'", entry["token"])
	}
}

// TestGatusExternalEndpointReconciler_WithHeartbeat verifies that a CR with a heartbeat
// configuration produces a heartbeat block in the output YAML.
func TestGatusExternalEndpointReconciler_WithHeartbeat(t *testing.T) {
	s := newTestScheme(t)
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "heartbeat-svc", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Heartbeat Service",
			Token: "tok-123",
			Heartbeat: &monitoringv1alpha1.GatusHeartbeatConfig{
				Interval: "30m",
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointConfigMap(), ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	reconcileExtEndpoint(t, r, "heartbeat-svc", "default")

	out := getExternalEndpointsYAML(t, fakeClient)
	endpoints := out["external-endpoints"].([]interface{})
	entry := endpoints[0].(map[string]interface{})
	heartbeat, ok := entry["heartbeat"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected heartbeat block, got: %v", entry["heartbeat"])
	}
	if heartbeat["interval"] != "30m" {
		t.Errorf("heartbeat.interval = %v, want 30m", heartbeat["interval"])
	}
}

// TestGatusExternalEndpointReconciler_MissingAlertRefGraceful verifies that when a CR
// references a GatusAlert that does not exist, the reconciler logs the error but still
// writes the external endpoint (without the missing alert) — no panic, no error returned.
func TestGatusExternalEndpointReconciler_MissingAlertRefGraceful(t *testing.T) {
	s := newTestScheme(t)
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-with-alert", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Service With Alert",
			Token: "tok-abc",
			Alerts: []monitoringv1alpha1.GatusAlertRef{
				{Name: "nonexistent-alert", Namespace: "default"},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointConfigMap(), ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	// Must not error out.
	reconcileExtEndpoint(t, r, "svc-with-alert", "default")

	out := getExternalEndpointsYAML(t, fakeClient)
	endpoints, ok := out["external-endpoints"].([]interface{})
	if !ok || len(endpoints) == 0 {
		t.Fatalf("expected endpoint to be written even when alert ref is missing, got: %v", out["external-endpoints"])
	}
	entry := endpoints[0].(map[string]interface{})
	if entry["name"] != "Service With Alert" {
		t.Errorf("name = %v, want 'Service With Alert'", entry["name"])
	}
	// Alerts should be empty since the referenced alert doesn't exist.
	if alerts, exists := entry["alerts"]; exists && alerts != nil {
		if list, ok := alerts.([]interface{}); ok && len(list) > 0 {
			t.Errorf("expected no alerts (missing ref), got: %v", alerts)
		}
	}
}

// TestGatusExternalEndpointReconciler_SpecialCharactersInToken verifies that a token
// containing YAML-special characters is marshaled into valid YAML.
func TestGatusExternalEndpointReconciler_SpecialCharactersInToken(t *testing.T) {
	s := newTestScheme(t)
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "special-token-svc", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Special Token Service",
			Token: `tok:special"value'with\backslash`,
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointConfigMap(), ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	reconcileExtEndpoint(t, r, "special-token-svc", "default")

	cm := &corev1.ConfigMap{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm)
	raw := cm.Data["external-endpoints.yaml"]

	// Must be valid YAML.
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("external-endpoints.yaml with special token is not valid YAML: %v\ncontent:\n%s", err, raw)
	}

	// Token must round-trip correctly.
	endpoints := out["external-endpoints"].([]interface{})
	entry := endpoints[0].(map[string]interface{})
	want := `tok:special"value'with\backslash`
	if entry["token"] != want {
		t.Errorf("token round-trip failed:\n got:  %v\n want: %v", entry["token"], want)
	}
}

// TestGatusExternalEndpointReconciler_RequeuesWhenConfigMapMissing verifies that
// reconciliation is requeued (not errored) when the target ConfigMap does not exist.
func TestGatusExternalEndpointReconciler_RequeuesWhenConfigMapMissing(t *testing.T) {
	s := newTestScheme(t)
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ext", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusExternalEndpointSpec{Name: "My Ext", Token: "tok"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-ext", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile must not return an error when ConfigMap is missing, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when ConfigMap is missing")
	}
}
