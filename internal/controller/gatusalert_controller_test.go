package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

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

func reconcileAlertExpectError(t *testing.T, r *GatusAlertReconciler, name, namespace string) error {
	t.Helper()
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
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

// TestGatusAlertReconciler_RetriggeredByAlertingConfigUpdate verifies that when
// the GatusAlertingConfig transitions to Valid=True, alertsForConfig returns a
// reconcile request for the referencing GatusAlert.
func TestGatusAlertReconciler_RetriggeredByAlertingConfigUpdate(t *testing.T) {
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "my-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{AlertingConfigRef: "slack-cfg"},
	}
	otherAlert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "other-alert", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{AlertingConfigRef: "discord-cfg"}, // different ref
	}
	alertingCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(alert, otherAlert, alertingCfg).
		Build()

	r := newAlertReconciler(fakeClient)
	requests := r.alertsForConfig(context.Background(), alertingCfg)

	if len(requests) != 1 {
		t.Fatalf("expected 1 reconcile request, got %d", len(requests))
	}
	if requests[0].Name != "my-slack" || requests[0].Namespace != "default" {
		t.Errorf("unexpected request: %+v", requests[0])
	}
}

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

// errInjectingClient wraps a client.Client and returns a given error when Get
// is called for objects of a specific GVK.
type errInjectingClient struct {
	client.Client
	gvk    schema.GroupVersionKind
	getErr error
}

func (c *errInjectingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	gvks, _, _ := c.Client.Scheme().ObjectKinds(obj)
	for _, g := range gvks {
		if g == c.gvk {
			return c.getErr
		}
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// TestGatusAlertReconciler_TransientErrorRequeues verifies that a transient (non-NotFound)
// error when fetching GatusAlertingConfig is returned (causing a requeue), not silently ignored.
func TestGatusAlertReconciler_TransientErrorRequeues(t *testing.T) {
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

	transientErr := apierrors.NewServiceUnavailable("apiserver unavailable")
	cfgGVK := schema.GroupVersionKind{
		Group:   monitoringv1alpha1.GroupVersion.Group,
		Version: monitoringv1alpha1.GroupVersion.Version,
		Kind:    "GatusAlertingConfig",
	}
	injectingClient := &errInjectingClient{
		Client: fakeClient,
		gvk:    cfgGVK,
		getErr: transientErr,
	}

	r := newAlertReconciler(injectingClient)
	err := reconcileAlertExpectError(t, r, "my-slack", "default")
	if err == nil {
		t.Fatal("expected error to be returned for transient Get failure, got nil")
	}
}

// TestGatusAlertReconciler_UpdateReflected verifies that when a GatusAlert's
// alertingConfigRef is changed, the status condition is updated after reconciliation.
func TestGatusAlertReconciler_UpdateReflected(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "my-slack", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{AlertingConfigRef: "slack-cfg"},
	}
	slackCfg := &monitoringv1alpha1.GatusAlertingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-cfg", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertingConfigSpec{Type: "slack"},
		Status: monitoringv1alpha1.GatusAlertingConfigStatus{
			Conditions: []metav1.Condition{{
				Type:               "Valid",
				Status:             metav1.ConditionTrue,
				Reason:             "ConfigValid",
				Message:            "All required fields are present",
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(alert, slackCfg).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlert{}, &monitoringv1alpha1.GatusAlertingConfig{}).
		Build()

	// Pre-set the status on slackCfg.
	if err := fakeClient.Status().Update(ctx, slackCfg); err != nil {
		t.Fatalf("failed to set status: %v", err)
	}

	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "my-slack", "default")

	cond := getAlertCondition(t, fakeClient, "my-slack", "default", "Configured")
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatal("expected Configured=True initially")
	}

	// Update alertingConfigRef to a non-existent config
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "my-slack", Namespace: "default"}, alert); err != nil {
		t.Fatalf("failed to re-fetch alert: %v", err)
	}
	alert.Spec.AlertingConfigRef = "does-not-exist"
	if err := fakeClient.Update(ctx, alert); err != nil {
		t.Fatalf("failed to update alert: %v", err)
	}

	reconcileAlert(t, r, "my-slack", "default")

	cond = getAlertCondition(t, fakeClient, "my-slack", "default", "Configured")
	if cond == nil {
		t.Fatal("expected Configured condition after update")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected Configured=False after updating to non-existent ref, got %v", cond.Status)
	}
}

// TestGatusAlertReconciler_DeletedAlertHandledGracefully verifies that deleting a
// GatusAlert and then reconciling it is handled gracefully (no error).
func TestGatusAlertReconciler_DeletedAlertHandledGracefully(t *testing.T) {
	ctx := context.Background()
	s := newTestScheme(t)
	alert := &monitoringv1alpha1.GatusAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "to-delete", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAlertSpec{AlertingConfigRef: "slack-cfg"},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(alert).
		WithStatusSubresource(&monitoringv1alpha1.GatusAlert{}).
		Build()

	r := newAlertReconciler(fakeClient)
	reconcileAlert(t, r, "to-delete", "default")

	// Delete the alert
	if err := fakeClient.Delete(ctx, alert); err != nil {
		t.Fatalf("failed to delete alert: %v", err)
	}

	// Reconcile again (triggered by delete event) — should not error.
	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "to-delete", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error after deleting alert, got: %v", err)
	}
}
