package controller

import (
	"context"
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func newExtEndpointReconciler(fakeClient client.Client) *GatusExternalEndpointReconciler {
	return &GatusExternalEndpointReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}
}

func extEndpointSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"},
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
	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, secret); err != nil {
		t.Fatalf("Secret not found: %v", err)
	}
	raw, ok := secret.Data["external-endpoints.yaml"]
	if !ok {
		t.Fatal("external-endpoints.yaml key not found in Secret")
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal(raw, &out); err != nil {
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
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), ext).Build()
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
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), ext).Build()
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
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), ext).Build()
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
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	reconcileExtEndpoint(t, r, "special-token-svc", "default")

	secret := &corev1.Secret{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, secret)
	raw := secret.Data["external-endpoints.yaml"]

	// Must be valid YAML.
	var out map[string]interface{}
	if err := yaml.Unmarshal(raw, &out); err != nil {
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

// TestGatusExternalEndpointReconciler_WithExistingAlertRef verifies that when a
// GatusExternalEndpoint references an existing GatusAlert, the alert appears in the output.
func TestGatusExternalEndpointReconciler_WithExistingAlertRef(t *testing.T) {
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "discord-alert", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			AlertingConfigRef: "discord-config",
			FailureThreshold:  3,
			SuccessThreshold:  2,
		},
	}
	discordAlertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "discord-config", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "discord"},
	}
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "ext-with-real-alert", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Ext With Real Alert",
			Token: "tok-123",
			Alerts: []monitoringv1alpha1.GatusAlertRef{
				{Name: "discord-alert"}, // no namespace → defaults to ext namespace
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), alert, discordAlertingCfg, ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	reconcileExtEndpoint(t, r, "ext-with-real-alert", "default")

	out := getExternalEndpointsYAML(t, fakeClient)
	endpoints := out["external-endpoints"].([]interface{})
	entry := endpoints[0].(map[string]interface{})
	alerts, ok := entry["alerts"].([]interface{})
	if !ok || len(alerts) == 0 {
		t.Fatalf("expected alerts list in output, got: %v", entry["alerts"])
	}
	alertEntry := alerts[0].(map[string]interface{})
	if alertEntry["type"] != "discord" {
		t.Errorf("alert type = %v, want 'discord'", alertEntry["type"])
	}
}

// TestGatusExternalEndpointReconciler_AlertRefOverrides verifies that per-endpoint
// alert overrides on GatusAlertRef are applied over GatusAlert defaults.
func TestGatusExternalEndpointReconciler_AlertRefOverrides(t *testing.T) {
	s := newTestScheme(t)
	trueVal := true
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "teams-alert", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			AlertingConfigRef:       "teams-config",
			FailureThreshold:        3,
			SuccessThreshold:        2,
			MinimumReminderInterval: "1h",
		},
	}
	teamsAlertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "teams-config", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "teams"},
	}
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "ext-override", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Ext Override",
			Token: "tok-456",
			Alerts: []monitoringv1alpha1.GatusAlertRef{
				{
					Name:                    "teams-alert",
					Namespace:               "default",
					Description:             "custom description",
					FailureThreshold:        9,
					SuccessThreshold:        4,
					SendOnResolved:          &trueVal,
					MinimumReminderInterval: "15m",
				},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), alert, teamsAlertingCfg, ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	reconcileExtEndpoint(t, r, "ext-override", "default")

	secret := &corev1.Secret{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, secret)
	raw := string(secret.Data["external-endpoints.yaml"])

	checks := map[string]string{
		"type: teams":                    "alert type",
		"description: custom":            "overridden description",
		"failure-threshold: 9":           "overridden failure-threshold",
		"success-threshold: 4":           "overridden success-threshold",
		"send-on-resolved: true":         "overridden send-on-resolved",
		"minimum-reminder-interval: 15m": "overridden minimum-reminder-interval",
	}
	for substr, label := range checks {
		if !contains(raw, substr) {
			t.Errorf("expected %s (%q) in external-endpoints.yaml, got:\n%s", label, substr, raw)
		}
	}
}

// TestGatusExternalEndpointReconciler_RequeuesWhenSecretMissing verifies that
// reconciliation is requeued (not errored) when the target Secret does not exist.
func TestGatusExternalEndpointReconciler_RequeuesWhenSecretMissing(t *testing.T) {
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
		t.Fatalf("Reconcile must not return an error when Secret is missing, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when Secret is missing")
	}
}

// TestGatusExternalEndpointReconciler_ProviderOverrideFromAlert verifies that
// GatusAlertSpec.ProviderOverride is rendered in the external-endpoints.yaml output.
func TestGatusExternalEndpointReconciler_ProviderOverrideFromAlert(t *testing.T) {
	s := newTestScheme(t)

	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "teams-override", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			AlertingConfigRef: "teams-config",
			FailureThreshold:  2,
			ProviderOverride: map[string]apiextv1.JSON{
				"webhook-url": {Raw: func() []byte { b, _ := json.Marshal("https://teams.example.com/webhook"); return b }()},
			},
		},
	}
	teamsAlertingCfg2 := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "teams-config", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "teams"},
	}
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "ext-override", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Ext Override",
			Token: "tok-override",
			Alerts: []monitoringv1alpha1.GatusAlertRef{
				{Name: "teams-override"},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), alert, teamsAlertingCfg2, ext).Build()
	r := newExtEndpointReconciler(fakeClient)
	reconcileExtEndpoint(t, r, "ext-override", "default")

	secret := &corev1.Secret{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, secret)
	raw := string(secret.Data["external-endpoints.yaml"])

	if !contains(raw, "provider-override") {
		t.Errorf("expected 'provider-override' in external-endpoints.yaml, got:\n%s", raw)
	}
	if !contains(raw, "teams.example.com") {
		t.Errorf("expected 'teams.example.com' in external-endpoints.yaml, got:\n%s", raw)
	}
}

// TestGatusExternalEndpointReconciler_DeletedEndpointRemoved verifies that deleting
// a GatusExternalEndpoint CR removes it from the Secret after reconciliation.
func TestGatusExternalEndpointReconciler_DeletedEndpointRemoved(t *testing.T) {
	s := newTestScheme(t)
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "to-delete", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Deletable Worker",
			Token: "tok-delete",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), ext).Build()
	r := newExtEndpointReconciler(fakeClient)

	// Create
	reconcileExtEndpoint(t, r, "to-delete", "default")
	out := getExternalEndpointsYAML(t, fakeClient)
	endpoints := out["external-endpoints"].([]interface{})
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}

	// Delete
	if err := fakeClient.Delete(context.Background(), ext); err != nil {
		t.Fatalf("failed to delete external endpoint: %v", err)
	}

	// Reconcile again
	reconcileExtEndpoint(t, r, "to-delete", "default")
	out = getExternalEndpointsYAML(t, fakeClient)
	endpoints = out["external-endpoints"].([]interface{})
	if len(endpoints) != 0 {
		t.Errorf("expected 0 endpoints after delete, got %d", len(endpoints))
	}
}

// TestGatusExternalEndpointReconciler_UpdateTokenReflected verifies that updating
// a GatusExternalEndpoint's token is reflected in the Secret after reconciliation.
func TestGatusExternalEndpointReconciler_UpdateTokenReflected(t *testing.T) {
	s := newTestScheme(t)
	ext := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "updatable-ext", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusExternalEndpointSpec{
			Name:  "Updatable Worker",
			Token: "original-token",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), ext).Build()
	r := newExtEndpointReconciler(fakeClient)

	reconcileExtEndpoint(t, r, "updatable-ext", "default")

	// Update
	ext.Spec.Token = "updated-token"
	ext.Spec.Group = "new-group"
	if err := fakeClient.Update(context.Background(), ext); err != nil {
		t.Fatalf("failed to update external endpoint: %v", err)
	}

	reconcileExtEndpoint(t, r, "updatable-ext", "default")

	secret := &corev1.Secret{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, secret)
	raw := string(secret.Data["external-endpoints.yaml"])

	if !contains(raw, "updated-token") {
		t.Errorf("expected updated token in output, got:\n%s", raw)
	}
	if !contains(raw, "new-group") {
		t.Errorf("expected updated group in output, got:\n%s", raw)
	}
}

// TestGatusExternalEndpointReconciler_DeterministicOrder verifies that multiple
// external endpoints produce deterministic YAML output regardless of creation order.
func TestGatusExternalEndpointReconciler_DeterministicOrder(t *testing.T) {
	s := newTestScheme(t)
	ext1 := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "zzz-worker", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusExternalEndpointSpec{Name: "ZZZ Worker", Token: "tok-z"},
	}
	ext2 := &monitoringv1alpha1.GatusExternalEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "aaa-worker", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusExternalEndpointSpec{Name: "AAA Worker", Token: "tok-a"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(extEndpointSecret(), ext1, ext2).Build()
	r := newExtEndpointReconciler(fakeClient)

	reconcileExtEndpoint(t, r, "zzz-worker", "default")

	out := getExternalEndpointsYAML(t, fakeClient)
	endpoints := out["external-endpoints"].([]interface{})
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}
	// First should be aaa-worker (alphabetical order)
	first := endpoints[0].(map[string]interface{})
	if first["name"] != "AAA Worker" {
		t.Errorf("expected first endpoint to be 'AAA Worker', got %v", first["name"])
	}
}
