package controller

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func newEndpointTestScheme(t *testing.T) *fake.ClientBuilder {
	t.Helper()
	s := newTestScheme(t)
	_ = clientgoscheme.AddToScheme(s)
	return fake.NewClientBuilder().WithScheme(s)
}

// TestGatusEndpointReconciler_WritesConfigMap verifies the basic happy path:
// all GatusEndpoint CRs are written into the ConfigMap under endpoints.yaml.
func TestGatusEndpointReconciler_WritesConfigMap(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-endpoint",
			Namespace: "default",
		},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name:       "My Service",
			Group:      "prod",
			URL:        "https://service.example.com",
			Conditions: []string{"[STATUS] == 200"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(secret, ep).
		Build()

	r := &GatusEndpointReconciler{
		Client:          fakeClient,
		Scheme:          s,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-endpoint", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated); err != nil {
		t.Fatalf("Secret not found: %v", err)
	}

	endpointsYAML, ok := updated.Data["endpoints.yaml"]
	if !ok {
		t.Fatal("endpoints.yaml key not found in Secret")
	}
	if !contains(string(endpointsYAML), "My Service") {
		t.Errorf("expected 'My Service' in endpoints.yaml, got:\n%s", string(endpointsYAML))
	}
	if !contains(string(endpointsYAML), "prod") {
		t.Errorf("expected group 'prod' in endpoints.yaml, got:\n%s", string(endpointsYAML))
	}
}

// TestGatusEndpointReconciler_RequeuesWhenConfigMapMissing verifies that reconciliation
// is requeued (not errored) when the target ConfigMap does not exist yet.
func TestGatusEndpointReconciler_RequeuesWhenConfigMapMissing(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-endpoint",
			Namespace: "default",
		},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "My Service",
			URL:  "https://service.example.com",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ep). // No Secret — intentionally absent. // No ConfigMap — intentionally absent.
		Build()

	r := &GatusEndpointReconciler{
		Client:          fakeClient,
		Scheme:          s,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-endpoint", Namespace: "default"}}
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile must not return an error when Secret is missing, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when Secret is missing")
	}
}

// TestGatusEndpointReconciler_DefaultCondition verifies that when a GatusEndpoint has no
// conditions defined, the generated endpoints.yaml contains the default [STATUS] == 200 check.
func TestGatusEndpointReconciler_DefaultCondition(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-conditions",
			Namespace: "default",
		},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "No Conditions Service",
			URL:  "https://service.example.com",
			// Conditions intentionally omitted.
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(secret, ep).
		Build()

	r := &GatusEndpointReconciler{
		Client:          fakeClient,
		Scheme:          s,
		TargetNamespace: "gatus",
		SecretName:      "gatus-secrets",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "no-conditions", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated); err != nil {
		t.Fatalf("Secret not found: %v", err)
	}

	endpointsYAML, ok := updated.Data["endpoints.yaml"]
	if !ok {
		t.Fatal("endpoints.yaml key not found in Secret")
	}

	if !contains(string(endpointsYAML), "[STATUS] == 200") {
		t.Errorf("expected default condition '[STATUS] == 200' in endpoints.yaml, got:\n%s", string(endpointsYAML))
	}
}

// TestGatusEndpointReconciler_ResolvesAlertRef verifies that when a GatusEndpoint
// references an existing GatusAlert, the resolved alert appears in endpoints.yaml.
func TestGatusEndpointReconciler_ResolvesAlertRef(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-alert", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			AlertingConfigRef: "slack-config",
			FailureThreshold:  3,
			SuccessThreshold:  2,
		},
	}
	alertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-config", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "My EP",
			URL:  "https://example.com",
			Alerts: []monitoringv1alpha1.GatusAlertRef{
				{Name: "slack-alert"}, // no namespace → defaults to endpoint namespace
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, alert, alertingCfg, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName:      "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret)
	yaml := string(updatedSecret.Data["endpoints.yaml"])

	if !contains(yaml, "type: slack") {
		t.Errorf("expected 'type: slack' in endpoints.yaml, got:\n%s", yaml)
	}
}

// TestGatusEndpointReconciler_AlertRefOverrides verifies that all per-endpoint overrides
// on a GatusAlertRef are applied correctly over the GatusAlert defaults.
func TestGatusEndpointReconciler_AlertRefOverrides(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	falseVal := false
	trueVal := true

	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "pagerduty-alert", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			AlertingConfigRef:       "pagerduty-config",
			Enabled:                 true,
			Description:             "default description",
			FailureThreshold:        3,
			SuccessThreshold:        2,
			SendOnResolved:          false,
			MinimumReminderInterval: "1h",
		},
	}
	pagerdutyAlertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "pagerduty-config", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "pagerduty"},
	}

	_ = falseVal
	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "override-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "Override EP",
			URL:  "https://example.com",
			Alerts: []monitoringv1alpha1.GatusAlertRef{
				{
					Name:                    "pagerduty-alert",
					Namespace:               "default",
					Description:             "endpoint-specific description",
					Enabled:                 &falseVal,
					FailureThreshold:        7,
					SuccessThreshold:        5,
					SendOnResolved:          &trueVal,
					MinimumReminderInterval: "30m",
				},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, alert, pagerdutyAlertingCfg, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName:      "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "override-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret3 := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret3)
	y := string(updatedSecret3.Data["endpoints.yaml"])

	checks := map[string]string{
		"type: pagerduty":                "alert type",
		"description: endpoint-specific": "overridden description",
		"failure-threshold: 7":           "overridden failure-threshold",
		"success-threshold: 5":           "overridden success-threshold",
		"send-on-resolved: true":         "overridden send-on-resolved",
		"minimum-reminder-interval: 30m": "overridden minimum-reminder-interval",
	}
	for substr, label := range checks {
		if !contains(y, substr) {
			t.Errorf("expected %s (%q) in endpoints.yaml, got:\n%s", label, substr, y)
		}
	}
	// The Enabled=false override: field should not appear (omitempty when false).
	if contains(y, "default description") {
		t.Errorf("default description should be overridden, got:\n%s", y)
	}
}

// TestGatusEndpointReconciler_WithClientConfig verifies that a GatusEndpoint with a
// client configuration (OAuth2 + TLS) produces the correct client block in endpoints.yaml.
func TestGatusEndpointReconciler_WithClientConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "client-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "Client EP",
			URL:  "https://example.com",
			Client: &monitoringv1alpha1.GatusClientConfig{
				Insecure: true,
				Timeout:  "10s",
				OAuth2: &monitoringv1alpha1.GatusClientOAuth2Config{
					TokenURL:     "https://auth.example.com/token",
					ClientID:     "my-client-id",
					ClientSecret: "my-secret",
					Scopes:       []string{"read", "write"},
				},
				TLS: &monitoringv1alpha1.GatusClientTLSConfig{
					CertificateFile: "/certs/tls.crt",
					PrivateKeyFile:  "/certs/tls.key",
				},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName:      "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "client-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret4 := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret4)
	y := string(updatedSecret4.Data["endpoints.yaml"])

	checks := map[string]string{
		"insecure: true":                          "insecure flag",
		"timeout: 10s":                            "timeout",
		"token-url: https://auth.example.com":     "OAuth2 token-url",
		"client-id: my-client-id":                 "OAuth2 client-id",
		"client-secret: my-secret":                "OAuth2 client-secret",
		"certificate-file: /certs/tls.crt":        "TLS certificate-file",
		"private-key-file: /certs/tls.key":        "TLS private-key-file",
	}
	for substr, label := range checks {
		if !contains(y, substr) {
			t.Errorf("expected %s (%q) in endpoints.yaml, got:\n%s", label, substr, y)
		}
	}
}

// TestUpsertConfigMapKey_NilData verifies that upsertConfigMapKey handles a ConfigMap
// where the Data map is nil (initializing it before writing the key).
func TestUpsertConfigMapKey_NilData(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	// ConfigMap exists but has no Data at all.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "gatus-config", Namespace: "gatus"},
		// Data intentionally nil.
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cm).Build()

	result, err := upsertConfigMapKey(ctx, fakeClient, "gatus", "gatus-config", "test.yaml", "hello: world\n")
	if err != nil {
		t.Fatalf("upsertConfigMapKey returned error: %v", err)
	}
	_ = result

	updated := &corev1.ConfigMap{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, updated)
	if updated.Data["test.yaml"] != "hello: world\n" {
		t.Errorf("expected test.yaml to be written, got Data=%v", updated.Data)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

func count(s, substr string) int {
	n := 0
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			n++
			i += len(substr) - 1
		}
	}
	return n
}

func makeAPIExtJSON(v interface{}) apiextv1.JSON {
	b, _ := json.Marshal(v)
	return apiextv1.JSON{Raw: b}
}

// TestGatusEndpointReconciler_ProviderOverrideFromAlert verifies that
// GatusAlertSpec.ProviderOverride is rendered in the endpoints.yaml output.
func TestGatusEndpointReconciler_ProviderOverrideFromAlert(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-override", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			AlertingConfigRef: "slack-config",
			FailureThreshold:  2,
			ProviderOverride: map[string]apiextv1.JSON{
				"username": makeAPIExtJSON("e2e-bot"),
			},
		},
	}
	slackAlertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-config", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "override-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "Override EP",
			URL:  "https://example.com",
			Alerts: []monitoringv1alpha1.GatusAlertRef{
				{Name: "slack-override"},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, alert, slackAlertingCfg, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "override-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret)
	y := string(updatedSecret.Data["endpoints.yaml"])

	if !contains(y, "provider-override") {
		t.Errorf("expected 'provider-override' in endpoints.yaml, got:\n%s", y)
	}
	if !contains(y, "e2e-bot") {
		t.Errorf("expected 'e2e-bot' in endpoints.yaml, got:\n%s", y)
	}
}

// TestGatusEndpointReconciler_ConflictDeduplication verifies that when two GatusEndpoints
// share the same spec.name, only the alphabetically first one is included in endpoints.yaml.
func TestGatusEndpointReconciler_ConflictDeduplication(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep1 := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "aaa-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "shared-name",
			URL:  "https://aaa.example.com",
		},
	}
	ep2 := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "zzz-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "shared-name",
			URL:  "https://zzz.example.com",
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep1, ep2).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	// Reconcile aaa-ep first
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "aaa-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile aaa-ep returned error: %v", err)
	}

	updatedSecret := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret)
	y := string(updatedSecret.Data["endpoints.yaml"])

	if !contains(y, "aaa.example.com") {
		t.Errorf("expected 'aaa.example.com' (winner) in endpoints.yaml, got:\n%s", y)
	}
	if count(y, "shared-name") > 1 {
		t.Errorf("expected only one 'shared-name' entry, got:\n%s", y)
	}
}
