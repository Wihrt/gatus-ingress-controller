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

func newAlertReconciler(fakeClient client.Client) *GatusAlertReconciler {
	return &GatusAlertReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		ConfigMapName:   "gatus-config",
	}
}

func alertConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "gatus-config", Namespace: "gatus"},
		Data:       map[string]string{"config.yaml": "web:\n  port: 8080\n"},
	}
}

func reconcileAlert(t *testing.T, r *GatusAlertReconciler, name, namespace string) ctrl.Result {
	t.Helper()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	if err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}
	return result
}

func getAlertingYAML(t *testing.T, fakeClient client.Client) map[string]interface{} {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm); err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}
	raw, ok := cm.Data["alerting.yaml"]
	if !ok {
		t.Fatal("alerting.yaml key not found in ConfigMap")
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("alerting.yaml is not valid YAML: %v\ncontent:\n%s", err, raw)
	}
	return out
}

// TestGatusAlertReconciler_WritesAlertingYAML verifies a single GatusAlert CR is
// marshaled and written to the alerting.yaml key in the ConfigMap.
func TestGatusAlertReconciler_WritesAlertingYAML(t *testing.T) {
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "my-slack", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertSpec{
			Type:             "slack",
			WebhookURL:       "https://hooks.slack.com/T000/B000/XXX",
			Enabled:          true,
			FailureThreshold: 3,
			SuccessThreshold: 2,
			SendOnResolved:   true,
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(alertConfigMap(), alert).Build()
	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "my-slack", "default")

	out := getAlertingYAML(t, fakeClient)
	alerting, ok := out["alerting"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected alerting map, got: %v", out["alerting"])
	}
	slackCfg, ok := alerting["slack"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected slack entry in alerting, keys: %v", alerting)
	}
	if slackCfg["webhook-url"] != "https://hooks.slack.com/T000/B000/XXX" {
		t.Errorf("webhook-url = %v, want the slack URL", slackCfg["webhook-url"])
	}
}

// TestGatusAlertReconciler_MultipleTypes verifies that two CRs with different provider
// types both appear in alerting.yaml.
func TestGatusAlertReconciler_MultipleTypes(t *testing.T) {
	s := newTestScheme(t)
	slackAlert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-alert", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{Type: "slack", WebhookURL: "https://hooks.slack.com/slack"},
	}
	discordAlert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "discord-alert", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{Type: "discord", WebhookURL: "https://discord.com/api/webhooks/discord"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(alertConfigMap(), slackAlert, discordAlert).Build()
	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "slack-alert", "default")

	out := getAlertingYAML(t, fakeClient)
	alerting := out["alerting"].(map[string]interface{})
	if _, ok := alerting["slack"]; !ok {
		t.Error("expected 'slack' entry in alerting.yaml")
	}
	if _, ok := alerting["discord"]; !ok {
		t.Error("expected 'discord' entry in alerting.yaml")
	}
}

// TestGatusAlertReconciler_DuplicateTypeFirstAlphabeticallyWins verifies that when two
// CRs have the same provider type, the one first alphabetically by namespace/name is used.
func TestGatusAlertReconciler_DuplicateTypeFirstAlphabeticallyWins(t *testing.T) {
	s := newTestScheme(t)
	first := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "aaa-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{Type: "slack", WebhookURL: "https://hooks.slack.com/aaa"},
	}
	second := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "zzz-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{Type: "slack", WebhookURL: "https://hooks.slack.com/zzz"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(alertConfigMap(), first, second).Build()
	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "aaa-slack", "default")

	out := getAlertingYAML(t, fakeClient)
	alerting := out["alerting"].(map[string]interface{})
	slackCfg := alerting["slack"].(map[string]interface{})
	if slackCfg["webhook-url"] != "https://hooks.slack.com/aaa" {
		t.Errorf("expected 'aaa' URL to win, got: %v", slackCfg["webhook-url"])
	}
}

// TestGatusAlertReconciler_EmptyTypeSkipped verifies that a CR with an empty type field
// is silently skipped — no panic, no entry in alerting.yaml.
func TestGatusAlertReconciler_EmptyTypeSkipped(t *testing.T) {
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "no-type", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{Type: "", WebhookURL: "https://example.com"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(alertConfigMap(), alert).Build()
	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "no-type", "default")

	out := getAlertingYAML(t, fakeClient)
	alerting, ok := out["alerting"].(map[string]interface{})
	if !ok {
		// alerting key may be null/nil when empty — that is acceptable
		return
	}
	if len(alerting) != 0 {
		t.Errorf("expected empty alerting map, got: %v", alerting)
	}
}

// TestGatusAlertReconciler_RequeuesWhenConfigMapMissing verifies that reconciliation
// is requeued (not errored) when the target ConfigMap does not exist yet.
func TestGatusAlertReconciler_RequeuesWhenConfigMapMissing(t *testing.T) {
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "my-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{Type: "slack"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(alert).Build()
	r := newAlertReconciler(fakeClient)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-slack", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile must not return an error when ConfigMap is missing, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when ConfigMap is missing")
	}
}
