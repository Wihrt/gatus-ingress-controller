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
// all GatusEndpoint CRs are written into the Secret under endpoints.yaml.
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
// is requeued (not errored) when the target Secret does not exist yet.
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
		WithObjects(ep). // No Secret — intentionally absent.
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

// TestGatusEndpointReconciler_InlineAlert verifies that a GatusEndpoint with an
// inline alert configuration produces the correct alert block in endpoints.yaml.
func TestGatusEndpointReconciler_InlineAlert(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	trueVal := true

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "My EP",
			URL:  "https://example.com",
			Alerts: []monitoringv1alpha1.GatusAlertSpec{
				{
					Type:             "slack",
					Enabled:          &trueVal,
					FailureThreshold: 3,
					SuccessThreshold: 2,
					SendOnResolved:   &trueVal,
					Description:      "endpoint down",
				},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret)
	y := string(updatedSecret.Data["endpoints.yaml"])

	checks := map[string]string{
		"type: slack":              "alert type",
		"failure-threshold: 3":    "failure-threshold",
		"success-threshold: 2":    "success-threshold",
		"send-on-resolved: true":  "send-on-resolved",
		"description: endpoint d": "description",
	}
	for substr, label := range checks {
		if !contains(y, substr) {
			t.Errorf("expected %s (%q) in endpoints.yaml, got:\n%s", label, substr, y)
		}
	}
}

// TestGatusEndpointReconciler_InlineAlertOverrides verifies that all inline alert fields
// including provider-override are rendered correctly in endpoints.yaml.
func TestGatusEndpointReconciler_InlineAlertOverrides(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	falseVal := false
	trueVal := true

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "override-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "Override EP",
			URL:  "https://example.com",
			Alerts: []monitoringv1alpha1.GatusAlertSpec{
				{
					Type:                    "pagerduty",
					Enabled:                 &falseVal,
					Description:             "endpoint-specific description",
					FailureThreshold:        7,
					SuccessThreshold:        5,
					SendOnResolved:          &trueVal,
					MinimumReminderInterval: "30m",
				},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "override-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret)
	y := string(updatedSecret.Data["endpoints.yaml"])

	checks := map[string]string{
		"type: pagerduty":                "alert type",
		"description: endpoint-specific": "overridden description",
		"failure-threshold: 7":           "overridden failure-threshold",
		"success-threshold: 5":           "overridden success-threshold",
		"send-on-resolved: true":         "overridden send-on-resolved",
		"minimum-reminder-interval: 30m": "overridden minimum-reminder-interval",
		"enabled: false":                 "enabled=false override",
	}
	for substr, label := range checks {
		if !contains(y, substr) {
			t.Errorf("expected %s (%q) in endpoints.yaml, got:\n%s", label, substr, y)
		}
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
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "client-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret4 := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret4)
	y := string(updatedSecret4.Data["endpoints.yaml"])

	checks := map[string]string{
		"insecure: true":                      "insecure flag",
		"timeout: 10s":                        "timeout",
		"token-url: https://auth.example.com": "OAuth2 token-url",
		"client-id: my-client-id":             "OAuth2 client-id",
		"client-secret: my-secret":            "OAuth2 client-secret",
		"certificate-file: /certs/tls.crt":    "TLS certificate-file",
		"private-key-file: /certs/tls.key":    "TLS private-key-file",
	}
	for substr, label := range checks {
		if !contains(y, substr) {
			t.Errorf("expected %s (%q) in endpoints.yaml, got:\n%s", label, substr, y)
		}
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

// TestGatusEndpointReconciler_ProviderOverride verifies that inline
// ProviderOverride is rendered in the endpoints.yaml output.
func TestGatusEndpointReconciler_ProviderOverride(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	trueVal := true

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "override-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "Override EP",
			URL:  "https://example.com",
			Alerts: []monitoringv1alpha1.GatusAlertSpec{
				{
					Type:             "slack",
					Enabled:          &trueVal,
					FailureThreshold: 2,
					ProviderOverride: map[string]apiextv1.JSON{
						"username": makeAPIExtJSON("e2e-bot"),
					},
				},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
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

// TestGatusEndpointReconciler_WithDNSConfig verifies that DNS config fields appear in endpoints.yaml.
func TestGatusEndpointReconciler_WithDNSConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name:       "DNS EP",
			URL:        "8.8.8.8",
			Conditions: []string{"[DNS_RCODE] == NOERROR"},
			DNS: &monitoringv1alpha1.GatusDNSConfig{
				QueryName: "example.com",
				QueryType: "A",
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "dns-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	y := string(updated.Data["endpoints.yaml"])

	for _, check := range []string{"query-name: example.com", "query-type: A"} {
		if !contains(y, check) {
			t.Errorf("expected %q in endpoints.yaml, got:\n%s", check, y)
		}
	}
}

// TestGatusEndpointReconciler_WithSSHConfig verifies that SSH config fields appear in endpoints.yaml.
func TestGatusEndpointReconciler_WithSSHConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "ssh-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name:       "SSH EP",
			URL:        "ssh://server.example.com:22",
			Conditions: []string{"[CONNECTED] == true"},
			SSH: &monitoringv1alpha1.GatusSSHConfig{
				Username: "admin",
				Password: "s3cret",
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "ssh-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	y := string(updated.Data["endpoints.yaml"])

	for _, check := range []string{"username: admin", "password: s3cret"} {
		if !contains(y, check) {
			t.Errorf("expected %q in endpoints.yaml, got:\n%s", check, y)
		}
	}
}

// TestGatusEndpointReconciler_WithUIConfig verifies that UI config fields appear in endpoints.yaml.
func TestGatusEndpointReconciler_WithUIConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "ui-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "UI EP",
			URL:  "https://example.com",
			UI: &monitoringv1alpha1.GatusUIConfig{
				HideConditions: true,
				HideHostname:   true,
				HideURL:        true,
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "ui-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	y := string(updated.Data["endpoints.yaml"])

	for _, check := range []string{"hide-conditions: true", "hide-hostname: true", "hide-url: true"} {
		if !contains(y, check) {
			t.Errorf("expected %q in endpoints.yaml, got:\n%s", check, y)
		}
	}
}

// TestGatusEndpointReconciler_WithMaintenanceWindows verifies that maintenance windows appear in endpoints.yaml.
func TestGatusEndpointReconciler_WithMaintenanceWindows(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "mw-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "MW EP",
			URL:  "https://example.com",
			MaintenanceWindows: []monitoringv1alpha1.GatusMaintenanceWindow{
				{
					Every:    []string{"Monday", "Friday"},
					Start:    "23:00",
					Duration: "1h",
					Timezone: "UTC",
				},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "mw-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	y := string(updated.Data["endpoints.yaml"])

	for _, check := range []string{"maintenance-windows:", "start: \"23:00\"", "duration: 1h", "timezone: UTC", "- Monday", "- Friday"} {
		if !contains(y, check) {
			t.Errorf("expected %q in endpoints.yaml, got:\n%s", check, y)
		}
	}
}

// TestGatusEndpointReconciler_NoAlerts verifies that an endpoint without alerts
// does not produce an alerts block in the output.
func TestGatusEndpointReconciler_NoAlerts(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "no-alerts-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "No Alerts EP",
			URL:  "https://example.com",
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "no-alerts-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	y := string(updated.Data["endpoints.yaml"])

	if !contains(y, "No Alerts EP") {
		t.Errorf("expected endpoint to be written, got:\n%s", y)
	}
	if contains(y, "alerts:") {
		t.Errorf("expected no alerts block when none specified, got:\n%s", y)
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

// TestGatusEndpointReconciler_DeletedEndpointRemovedFromSecret verifies that when a
// GatusEndpoint is deleted, the reconciler re-aggregates and the endpoint is no longer
// present in endpoints.yaml.
func TestGatusEndpointReconciler_DeletedEndpointRemovedFromSecret(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "to-delete", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name:       "Deletable Service",
			URL:        "https://delete-me.example.com",
			Conditions: []string{"[STATUS] == 200"},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	// Create
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "to-delete", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	if !contains(string(updated.Data["endpoints.yaml"]), "Deletable Service") {
		t.Fatal("expected endpoint to be present before deletion")
	}

	// Delete the CR
	if err := fakeClient.Delete(ctx, ep); err != nil {
		t.Fatalf("failed to delete endpoint: %v", err)
	}

	// Reconcile again (triggered by delete event)
	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "to-delete", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile after delete returned error: %v", err)
	}

	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	if contains(string(updated.Data["endpoints.yaml"]), "Deletable Service") {
		t.Errorf("expected endpoint to be removed after deletion, got:\n%s", string(updated.Data["endpoints.yaml"]))
	}
}

// TestGatusEndpointReconciler_UpdateReflectedInSecret verifies that updating a
// GatusEndpoint spec is reflected in the Secret after reconciliation.
func TestGatusEndpointReconciler_UpdateReflectedInSecret(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "updatable-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name:       "Original Name",
			URL:        "https://original.example.com",
			Conditions: []string{"[STATUS] == 200"},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	// Initial reconcile
	_, _ = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "updatable-ep", Namespace: "default"}})

	// Update the CR
	ep.Spec.URL = "https://updated.example.com"
	ep.Spec.Name = "Updated Name"
	if err := fakeClient.Update(ctx, ep); err != nil {
		t.Fatalf("failed to update endpoint: %v", err)
	}

	// Reconcile again
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "updatable-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile after update returned error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updated)
	y := string(updated.Data["endpoints.yaml"])
	if !contains(y, "updated.example.com") {
		t.Errorf("expected updated URL in endpoints.yaml, got:\n%s", y)
	}
	if !contains(y, "Updated Name") {
		t.Errorf("expected updated name in endpoints.yaml, got:\n%s", y)
	}
}

// TestGatusEndpointReconciler_MultipleAlertTypes verifies that multiple inline alerts
// with different types are all rendered in the endpoints.yaml output.
func TestGatusEndpointReconciler_MultipleAlertTypes(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	trueVal := true

	ep := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "multi-alert-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "Multi Alert EP",
			URL:  "https://example.com",
			Alerts: []monitoringv1alpha1.GatusAlertSpec{
				{Type: "slack", Enabled: &trueVal, FailureThreshold: 3},
				{Type: "discord", Enabled: &trueVal, FailureThreshold: 5},
				{Type: "pagerduty", Enabled: &trueVal, FailureThreshold: 1},
			},
		},
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gatus-secrets", Namespace: "gatus"}}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret, ep).Build()
	r := &GatusEndpointReconciler{Client: fakeClient, Scheme: s, TargetNamespace: "gatus", SecretName: "gatus-secrets"}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "multi-alert-ep", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSecret := &corev1.Secret{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "gatus-secrets", Namespace: "gatus"}, updatedSecret)
	y := string(updatedSecret.Data["endpoints.yaml"])

	for _, alertType := range []string{"slack", "discord", "pagerduty"} {
		if !contains(y, "type: "+alertType) {
			t.Errorf("expected 'type: %s' in endpoints.yaml, got:\n%s", alertType, y)
		}
	}
}
