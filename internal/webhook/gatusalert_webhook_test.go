package webhook

import (
	"context"
	"encoding/json"
	"testing"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func newAlertWebhookScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = monitoringv1alpha1.AddToScheme(s)
	return s
}

func makeAlertingConfig(name, ns, providerType string) *monitoringv1alpha1.GatusAlertingConfig {
	return &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: providerType},
	}
}

func makeAlert(alertingConfigRef string, overrides map[string]interface{}) *monitoringv1alpha1.GatusAlert {
	po := make(map[string]apiextv1.JSON, len(overrides))
	for k, v := range overrides {
		b, _ := json.Marshal(v)
		po[k] = apiextv1.JSON{Raw: b}
	}
	return &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "test-alert", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			AlertingConfigRef: alertingConfigRef,
			ProviderOverride:  po,
		},
	}
}

func makeAlertValidator(t *testing.T, configs ...*monitoringv1alpha1.GatusAlertingConfig) *GatusAlertValidator {
	t.Helper()
	s := newAlertWebhookScheme(t)
	clientObjs := make([]client.Object, len(configs))
	for i, c := range configs {
		clientObjs[i] = c
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(clientObjs...).Build()
	return &GatusAlertValidator{Client: fakeClient}
}

func TestAlertWebhook_AllowsEmptyOverride(t *testing.T) {
	cfg := makeAlertingConfig("discord-config", "default", "discord")
	v := makeAlertValidator(t, cfg)
	alert := makeAlert("discord-config", nil)
	_, err := v.ValidateCreate(context.Background(), alert)
	if err != nil {
		t.Errorf("expected alert with no overrides to be accepted, got: %v", err)
	}
}

func TestAlertWebhook_AllowsValidDiscordOverride(t *testing.T) {
	cfg := makeAlertingConfig("discord-config", "default", "discord")
	v := makeAlertValidator(t, cfg)
	alert := makeAlert("discord-config", map[string]interface{}{
		"webhook-url": "https://discord.com/api/webhooks/xxx",
	})
	_, err := v.ValidateCreate(context.Background(), alert)
	if err != nil {
		t.Errorf("expected valid discord override to be accepted, got: %v", err)
	}
}

func TestAlertWebhook_RejectsUnknownOverrideKey(t *testing.T) {
	cfg := makeAlertingConfig("discord-config", "default", "discord")
	v := makeAlertValidator(t, cfg)
	alert := makeAlert("discord-config", map[string]interface{}{
		"bad-key": "value",
	})
	_, err := v.ValidateCreate(context.Background(), alert)
	if err == nil {
		t.Error("expected unknown override key to be rejected")
	}
}

func TestAlertWebhook_ValidateUpdate_SameAsCreate(t *testing.T) {
	cfg := makeAlertingConfig("discord-config", "default", "discord")
	v := makeAlertValidator(t, cfg)
	old := makeAlert("discord-config", nil)
	updated := makeAlert("discord-config", map[string]interface{}{
		"bad-key": "value",
	})
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Error("expected invalid update to be rejected")
	}
}

func TestAlertWebhook_ValidateDelete(t *testing.T) {
	v := &GatusAlertValidator{}
	alert := makeAlert("discord-config", nil)
	_, err := v.ValidateDelete(context.Background(), alert)
	if err != nil {
		t.Errorf("ValidateDelete should always succeed, got: %v", err)
	}
}

func TestAlertWebhook_AllowsSlackOverride(t *testing.T) {
	cfg := makeAlertingConfig("slack-config", "default", "slack")
	v := makeAlertValidator(t, cfg)
	alert := makeAlert("slack-config", map[string]interface{}{
		"webhook-url": "https://hooks.slack.com/services/xxx",
	})
	_, err := v.ValidateCreate(context.Background(), alert)
	if err != nil {
		t.Errorf("expected valid slack override to be accepted, got: %v", err)
	}
}

func TestAlertWebhook_RejectsNotFoundAlertingConfigRef(t *testing.T) {
	v := makeAlertValidator(t) // no configs in client
	alert := makeAlert("nonexistent-config", map[string]interface{}{
		"webhook-url": "https://hooks.slack.com/services/xxx",
	})
	_, err := v.ValidateCreate(context.Background(), alert)
	if err == nil {
		t.Error("expected error when referenced GatusAlertingConfig does not exist")
	}
}

func TestAlertWebhook_RejectsMissingAlertingConfigRef(t *testing.T) {
	v := makeAlertValidator(t)
	alert := makeAlert("", nil) // empty ref
	_, err := v.ValidateCreate(context.Background(), alert)
	if err == nil {
		t.Error("expected error when alertingConfigRef is empty")
	}
}

// TestAlertWebhook_RejectsNotFoundRefWithNoOverrides verifies that a non-empty
// alertingConfigRef pointing to a missing config is rejected even without overrides.
func TestAlertWebhook_RejectsNotFoundRefWithNoOverrides(t *testing.T) {
	v := makeAlertValidator(t) // no configs in client
	alert := makeAlert("nonexistent-config", nil)
	_, err := v.ValidateCreate(context.Background(), alert)
	if err == nil {
		t.Error("expected error when alertingConfigRef points to missing config with no overrides")
	}
}
