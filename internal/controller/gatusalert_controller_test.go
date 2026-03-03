package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func newAlertReconciler(fakeClient client.Client) *GatusAlertReconciler {
	return &GatusAlertReconciler{
		Client: fakeClient,
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

func getAlertCondition(t *testing.T, fakeClient client.Client, name, namespace, condType string) *metav1.Condition {
	t.Helper()
	alert := &monitoringv1alpha1.GatusAlert{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, alert); err != nil {
		t.Fatalf("GatusAlert not found: %v", err)
	}
	for i := range alert.Status.Conditions {
		if alert.Status.Conditions[i].Type == condType {
			return &alert.Status.Conditions[i]
		}
	}
	return nil
}

// TestGatusAlertReconciler_ConfiguredTrue verifies that when a matching valid
// GatusAlertingConfig exists (with Valid=True status condition), the GatusAlert gets Configured=True.
func TestGatusAlertReconciler_ConfiguredTrue(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "my-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{AlertingConfigRef: "slack-cfg"},
	}
	alertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAlertingConfigSpec{
			Type: "slack",
			Config: map[string]apiextv1.JSON{
				"webhook-url": {Raw: []byte(`"https://hooks.slack.com/T000/B000/XXX"`)},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(alert, alertingCfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlert{}, &monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	// Pre-set Valid=True on the GatusAlertingConfig (normally done by GatusAlertingConfigReconciler).
	alertingCfg.Status.Conditions = []metav1.Condition{{
		Type:               "Valid",
		Status:             metav1.ConditionTrue,
		Reason:             "ConfigValid",
		Message:            "All required fields are present",
		LastTransitionTime: metav1.Now(),
	}}
	if err := fakeClient.Status().Update(ctx, alertingCfg); err != nil {
		t.Fatalf("failed to set GatusAlertingConfig status: %v", err)
	}

	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "my-slack", "default")

	cond := getAlertCondition(t, fakeClient, "my-slack", "default", "Configured")
	if cond == nil {
		t.Fatal("expected Configured condition to be set")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected Configured=True, got %v: %s", cond.Status, cond.Message)
	}
}

// TestGatusAlertReconciler_ConfiguredFalse verifies that when no matching
// GatusAlertingConfig exists, the GatusAlert gets Configured=False.
func TestGatusAlertReconciler_ConfiguredFalse(t *testing.T) {
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "my-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{AlertingConfigRef: "slack-cfg"},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(alert).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlert{}).
		Build()

	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "my-slack", "default")

	cond := getAlertCondition(t, fakeClient, "my-slack", "default", "Configured")
	if cond == nil {
		t.Fatal("expected Configured condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected Configured=False, got %v", cond.Status)
	}
}

// TestGatusAlertReconciler_NotFound verifies that a missing GatusAlert is handled gracefully.
func TestGatusAlertReconciler_NotFound(t *testing.T) {
	s := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
	r := newAlertReconciler(fakeClient)
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "does-not-exist", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error for not-found alert, got: %v", err)
	}
}

// TestGatusAlertReconciler_InvalidAlertingConfigNotCounted verifies that an invalid
// GatusAlertingConfig (missing required fields) does not count as "configured".
func TestGatusAlertReconciler_InvalidAlertingConfigNotCounted(t *testing.T) {
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "my-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{AlertingConfigRef: "slack-cfg"},
	}
	// Missing required webhook-url field.
	alertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(alert, alertingCfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlert{}).
		Build()

	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "my-slack", "default")

	cond := getAlertCondition(t, fakeClient, "my-slack", "default", "Configured")
	if cond == nil {
		t.Fatal("expected Configured condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected Configured=False when alerting config is invalid, got %v", cond.Status)
	}
}
