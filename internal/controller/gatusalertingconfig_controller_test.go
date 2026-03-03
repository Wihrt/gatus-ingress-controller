package controller

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func mustJSON(v interface{}) apiextv1.JSON {
	b, _ := json.Marshal(v)
	return apiextv1.JSON{Raw: b}
}

func makeAlertingConfig(name, ns, providerType string, config map[string]interface{}) *monitoringv1alpha1.GatusAlertingConfig {
	c := make(map[string]apiextv1.JSON, len(config))
	for k, v := range config {
		c[k] = mustJSON(v)
	}
	return &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type:   providerType,
			Config: c,
		},
	}
}

func TestGatusAlertingConfigReconciler_ValidConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	cfg := makeAlertingConfig("slack-cfg", "default", "slack", map[string]interface{}{
		"webhook-url": "https://hooks.slack.com/services/xxx",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(secret, cfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	r := &GatusAlertingConfigReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "slack-cfg", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Check status condition Valid=True.
	updated := &monitoringv1alpha1.GatusAlertingConfig{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "slack-cfg", Namespace: "default"}, updated); err != nil {
		t.Fatalf("GatusAlertingConfig not found: %v", err)
	}
	var validCond *metav1.Condition
	for i := range updated.Status.Conditions {
		if updated.Status.Conditions[i].Type == "Valid" {
			validCond = &updated.Status.Conditions[i]
		}
	}
	if validCond == nil {
		t.Fatal("expected 'Valid' condition, not found")
	}
	if validCond.Status != metav1.ConditionTrue {
		t.Errorf("expected Valid=True, got %s: %s", validCond.Status, validCond.Message)
	}

	// Check alerting.yaml was written to Secret.
	updatedSecret := &corev1.Secret{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret); err != nil {
		t.Fatalf("Secret not found: %v", err)
	}
	alertingYAML, ok := updatedSecret.Data[alertingKey]
	if !ok {
		t.Fatal("alerting.yaml key not found in Secret")
	}
	if !contains(string(alertingYAML), "slack") {
		t.Errorf("expected 'slack' in alerting.yaml, got:\n%s", string(alertingYAML))
	}
	if !contains(string(alertingYAML), "webhook-url") {
		t.Errorf("expected 'webhook-url' in alerting.yaml, got:\n%s", string(alertingYAML))
	}
}

func TestGatusAlertingConfigReconciler_InvalidConfig_MissingFields(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	// Slack requires webhook-url; omit it to trigger validation failure.
	cfg := makeAlertingConfig("slack-invalid", "default", "slack", map[string]interface{}{})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(secret, cfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	r := &GatusAlertingConfigReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "slack-invalid", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Check status condition Valid=False.
	updated := &monitoringv1alpha1.GatusAlertingConfig{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "slack-invalid", Namespace: "default"}, updated); err != nil {
		t.Fatalf("GatusAlertingConfig not found: %v", err)
	}
	var validCond *metav1.Condition
	for i := range updated.Status.Conditions {
		if updated.Status.Conditions[i].Type == "Valid" {
			validCond = &updated.Status.Conditions[i]
		}
	}
	if validCond == nil {
		t.Fatal("expected 'Valid' condition, not found")
	}
	if validCond.Status != metav1.ConditionFalse {
		t.Errorf("expected Valid=False, got %s", validCond.Status)
	}
	if !contains(validCond.Message, "webhook-url") {
		t.Errorf("expected 'webhook-url' in condition message, got: %s", validCond.Message)
	}

	// alerting.yaml must NOT have been written.
	updatedSecret := &corev1.Secret{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret); err != nil {
		t.Fatalf("Secret not found: %v", err)
	}
	if _, ok := updatedSecret.Data[alertingKey]; ok {
		t.Error("alerting.yaml should NOT be written for invalid config")
	}
}

func TestGatusAlertingConfigReconciler_DeduplicatesByType(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	// Two valid Slack configs — first alphabetically (aaa-slack) should win.
	cfg1 := makeAlertingConfig("aaa-slack", "default", "slack", map[string]interface{}{
		"webhook-url": "https://hooks.slack.com/services/aaa",
	})
	cfg2 := makeAlertingConfig("zzz-slack", "default", "slack", map[string]interface{}{
		"webhook-url": "https://hooks.slack.com/services/zzz",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(secret, cfg1, cfg2).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	r := &GatusAlertingConfigReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "aaa-slack", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret := &corev1.Secret{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret); err != nil {
		t.Fatalf("Secret not found: %v", err)
	}
	alertingYAML := string(updatedSecret.Data[alertingKey])
	if !contains(alertingYAML, "aaa") {
		t.Errorf("expected 'aaa' webhook (first alphabetically) in alerting.yaml, got:\n%s", alertingYAML)
	}
	if contains(alertingYAML, "zzz") {
		t.Errorf("'zzz' webhook (duplicate) must not appear in alerting.yaml, got:\n%s", alertingYAML)
	}
}

func TestGatusAlertingConfigReconciler_MissingSecret_Requeues(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	// No Secret created — controller should requeue, not error.
	cfg := makeAlertingConfig("slack-cfg", "default", "slack", map[string]interface{}{
		"webhook-url": "https://hooks.slack.com/services/xxx",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(cfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	r := &GatusAlertingConfigReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "slack-cfg", Namespace: "default"}})
	if err != nil {
		t.Fatalf("expected no error (requeue), got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected a non-zero RequeueAfter when Secret is missing")
	}
}

func TestUpsertSecretKey_NilData(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	// Secret exists but has no Data at all.
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"},
		// Data intentionally nil.
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(sec).Build()

	result, err := upsertSecretKey(ctx, fakeClient, "gatus", "gatus-secrets", "test.yaml", "hello: world\n")
	if err != nil {
		t.Fatalf("upsertSecretKey returned error: %v", err)
	}
	_ = result

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	if string(updated.Data["test.yaml"]) != "hello: world\n" {
		t.Errorf("expected test.yaml to be written, got Data=%v", updated.Data)
	}
}

// TestGatusAlertingConfigReconciler_ConfigSecretRef_MergesSecret verifies that when
// configSecretRef is set, its data is merged into the provider config (secret wins for
// same key) and the result is written to alerting.yaml.
func TestGatusAlertingConfigReconciler_ConfigSecretRef_MergesSecret(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	outputSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}

	// The webhook-url comes from the referenced secret, not from spec.config.
	providerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-credentials", Namespace: "controller-ns"},
		Data: map[string][]byte{
			"webhook-url": []byte("https://hooks.slack.com/services/from-secret"),
		},
	}

	cfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type:   "slack",
			Config: map[string]apiextv1.JSON{},
			ConfigSecretRef: &monitoringv1alpha1.ConfigSecretRef{
				Name: "slack-credentials",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(outputSecret, providerSecret, cfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	r := &GatusAlertingConfigReconciler{
		Client:              fakeClient,
		TargetNamespace:     "gatus",
		SecretName:          "gatus-secrets",
		ControllerNamespace: "controller-ns",
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "slack-cfg", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Validate=True because the secret provided the required webhook-url.
	updated := &monitoringv1alpha1.GatusAlertingConfig{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "slack-cfg", Namespace: "default"}, updated); err != nil {
		t.Fatalf("GatusAlertingConfig not found: %v", err)
	}
	var validCond *metav1.Condition
	for i := range updated.Status.Conditions {
		if updated.Status.Conditions[i].Type == "Valid" {
			validCond = &updated.Status.Conditions[i]
		}
	}
	if validCond == nil || validCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Valid=True after secret merge, got %v", validCond)
	}

	// alerting.yaml must contain the value from the secret.
	updatedOutput := &corev1.Secret{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedOutput); err != nil {
		t.Fatalf("output secret not found: %v", err)
	}
	alertingYAML := string(updatedOutput.Data[alertingKey])
	if !contains(alertingYAML, "from-secret") {
		t.Errorf("expected webhook URL from secret in alerting.yaml, got:\n%s", alertingYAML)
	}
}

// TestGatusAlertingConfigReconciler_ConfigSecretRef_SecretWinsOverInline verifies that
// a key present in both spec.config and the referenced secret uses the secret value.
func TestGatusAlertingConfigReconciler_ConfigSecretRef_SecretWinsOverInline(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	outputSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}

	providerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-credentials", Namespace: "controller-ns"},
		Data: map[string][]byte{
			"webhook-url": []byte("https://hooks.slack.com/services/from-secret"),
		},
	}

	cfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type: "slack",
			Config: map[string]apiextv1.JSON{
				// This value should be overridden by the secret.
				"webhook-url": mustJSON("https://hooks.slack.com/services/inline-value"),
			},
			ConfigSecretRef: &monitoringv1alpha1.ConfigSecretRef{
				Name: "slack-credentials",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(outputSecret, providerSecret, cfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	r := &GatusAlertingConfigReconciler{
		Client:              fakeClient,
		TargetNamespace:     "gatus",
		SecretName:          "gatus-secrets",
		ControllerNamespace: "controller-ns",
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "slack-cfg", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedOutput := &corev1.Secret{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedOutput); err != nil {
		t.Fatalf("output secret not found: %v", err)
	}
	alertingYAML := string(updatedOutput.Data[alertingKey])
	if !contains(alertingYAML, "from-secret") {
		t.Errorf("expected secret value to win, got:\n%s", alertingYAML)
	}
	if contains(alertingYAML, "inline-value") {
		t.Errorf("inline value should be overridden by secret, got:\n%s", alertingYAML)
	}
}

// TestGatusAlertingConfigReconciler_ConfigSecretRef_MissingSecret_Requeues verifies
// that when the referenced configSecretRef Secret does not exist, reconciliation requeues.
func TestGatusAlertingConfigReconciler_ConfigSecretRef_MissingSecret_Requeues(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	outputSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}

	cfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type:   "slack",
			Config: map[string]apiextv1.JSON{},
			ConfigSecretRef: &monitoringv1alpha1.ConfigSecretRef{
				Name: "does-not-exist",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(outputSecret, cfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	r := &GatusAlertingConfigReconciler{
		Client:              fakeClient,
		TargetNamespace:     "gatus",
		SecretName:          "gatus-secrets",
		ControllerNamespace: "controller-ns",
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "slack-cfg", Namespace: "default"}})
	if err != nil {
		t.Fatalf("expected no error (requeue), got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected a non-zero RequeueAfter when configSecretRef Secret is missing")
	}
}
