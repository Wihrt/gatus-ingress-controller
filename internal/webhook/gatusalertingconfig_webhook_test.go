package webhook

import (
	"context"
	"encoding/json"
	"testing"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func newConfigWebhookScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = monitoringv1alpha1.AddToScheme(s)
	return s
}

func makeConfig(providerType string, config map[string]interface{}, secretRef *monitoringv1alpha1.ConfigSecretRef) *monitoringv1alpha1.GatusAlertingConfig {
	c := make(map[string]apiextv1.JSON, len(config))
	for k, v := range config {
		b, _ := json.Marshal(v)
		c[k] = apiextv1.JSON{Raw: b}
	}
	return &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type:            providerType,
			Config:          c,
			ConfigSecretRef: secretRef,
		},
	}
}

func TestWebhook_AllowsValidDiscordConfig(t *testing.T) {
	v := &GatusAlertingConfigValidator{}
	cfg := makeConfig("discord", map[string]interface{}{
		"webhook-url": "https://discord.com/api/webhooks/xxx",
	}, nil)
	_, err := v.ValidateCreate(context.Background(), cfg)
	if err != nil {
		t.Errorf("expected valid discord config to be accepted, got: %v", err)
	}
}

func TestWebhook_RejectsUnknownField(t *testing.T) {
	v := &GatusAlertingConfigValidator{}
	cfg := makeConfig("discord", map[string]interface{}{
		"webhook-url": "https://discord.com/api/webhooks/xxx",
		"not-a-field": "garbage",
	}, nil)
	_, err := v.ValidateCreate(context.Background(), cfg)
	if err == nil {
		t.Error("expected unknown field to be rejected")
	}
}

func TestWebhook_RejectsMissingRequiredFieldWithoutSecretRef(t *testing.T) {
	v := &GatusAlertingConfigValidator{}
	// slack requires webhook-url but it's absent and no configSecretRef is set
	cfg := makeConfig("slack", map[string]interface{}{
		"title": "My Alerts",
	}, nil)
	_, err := v.ValidateCreate(context.Background(), cfg)
	if err == nil {
		t.Error("expected missing required field to be rejected when no configSecretRef")
	}
}

func TestWebhook_AllowsMissingRequiredFieldWithSecretRef(t *testing.T) {
	v := &GatusAlertingConfigValidator{}
	// webhook-url is missing from spec.config but configSecretRef is set — should be accepted
	cfg := makeConfig("slack", map[string]interface{}{
		"title": "My Alerts",
	}, &monitoringv1alpha1.ConfigSecretRef{Name: "slack-secret"})
	_, err := v.ValidateCreate(context.Background(), cfg)
	if err != nil {
		t.Errorf("expected config with missing required field + configSecretRef to be accepted, got: %v", err)
	}
}

func TestWebhook_AllowsValidEmailConfig(t *testing.T) {
	v := &GatusAlertingConfigValidator{}
	cfg := makeConfig("email", map[string]interface{}{
		"from":     "gatus@example.com",
		"host":     "smtp.example.com",
		"port":     587,
		"to":       "ops@example.com",
		"username": "gatus",
		"password": "secret",
	}, nil)
	_, err := v.ValidateCreate(context.Background(), cfg)
	if err != nil {
		t.Errorf("expected valid email config to be accepted, got: %v", err)
	}
}

func TestWebhook_ValidateUpdate_SameAsCreate(t *testing.T) {
	v := &GatusAlertingConfigValidator{}
	old := makeConfig("discord", map[string]interface{}{
		"webhook-url": "https://discord.com/api/webhooks/old",
	}, nil)
	newCfg := makeConfig("discord", map[string]interface{}{
		"webhook-url": "https://discord.com/api/webhooks/new",
		"bad-field":   "nope",
	}, nil)
	_, err := v.ValidateUpdate(context.Background(), old, newCfg)
	if err == nil {
		t.Error("expected update with unknown field to be rejected")
	}
}

func TestWebhook_ValidateDelete_AlwaysAllowed(t *testing.T) {
	v := &GatusAlertingConfigValidator{}
	cfg := makeConfig("discord", nil, nil)
	_, err := v.ValidateDelete(context.Background(), cfg)
	if err != nil {
		t.Errorf("expected delete to always be allowed, got: %v", err)
	}
}

func TestWebhook_RejectsSecondConfigOfSameType(t *testing.T) {
	s := newConfigWebhookScheme(t)
	existing := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existing).Build()
	v := &GatusAlertingConfigValidator{Client: fakeClient}

	newCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "new-slack", Namespace: "other"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}
	_, err := v.ValidateCreate(context.Background(), newCfg)
	if err == nil {
		t.Error("expected rejection when a second slack config is created")
	}
}

func TestWebhook_AllowsUpdateOfExistingConfig(t *testing.T) {
	s := newConfigWebhookScheme(t)
	existing := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type: "slack",
			Config: map[string]apiextv1.JSON{
				"webhook-url": {Raw: func() []byte { b, _ := json.Marshal("https://hooks.slack.com/old"); return b }()},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existing).Build()
	v := &GatusAlertingConfigValidator{Client: fakeClient}

	updated := existing.DeepCopy()
	updated.Spec.Config["webhook-url"] = apiextv1.JSON{Raw: func() []byte { b, _ := json.Marshal("https://hooks.slack.com/new"); return b }()}
	_, err := v.ValidateUpdate(context.Background(), existing, updated)
	if err != nil {
		t.Errorf("expected updating existing slack config to be allowed, got: %v", err)
	}
}

func TestWebhook_AllowsCreateOfDifferentType(t *testing.T) {
	s := newConfigWebhookScheme(t)
	existing := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existing).Build()
	v := &GatusAlertingConfigValidator{Client: fakeClient}

	discordCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "discord-cfg", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type: "discord",
			Config: map[string]apiextv1.JSON{
				"webhook-url": {Raw: func() []byte { b, _ := json.Marshal("https://discord.com/webhooks/xxx"); return b }()},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), discordCfg)
	if err != nil {
		t.Errorf("expected creating a discord config when slack exists to be allowed, got: %v", err)
	}
}

// TestWebhook_UniquenessFailsClosedOnListError verifies that validateUniqueness
// returns an error (fail-closed) when the client List call fails.
func TestWebhook_UniquenessFailsClosedOnListError(t *testing.T) {
	// Use a scheme without GatusAlertingConfig registered so List() fails
	brokenScheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(brokenScheme)
	fakeClient := fake.NewClientBuilder().WithScheme(brokenScheme).Build()
	v := &GatusAlertingConfigValidator{Client: fakeClient}

	cfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cfg", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}
	errs := v.validateUniqueness(context.Background(), cfg, "")
	if len(errs) == 0 {
		t.Error("expected validateUniqueness to fail closed when List() errors, got no errors")
	}
}
