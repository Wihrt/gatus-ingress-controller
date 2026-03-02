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

func newMaintenanceReconciler(fakeClient client.Client) *GatusMaintenanceReconciler {
	return &GatusMaintenanceReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		ConfigMapName:   "gatus-config",
	}
}

func maintenanceConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "gatus-config", Namespace: "gatus"},
		Data:       map[string]string{"config.yaml": "web:\n  port: 8080\n"},
	}
}

func reconcileMaintenance(t *testing.T, r *GatusMaintenanceReconciler, name, namespace string) ctrl.Result {
	t.Helper()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	if err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}
	return result
}

func getMaintenanceYAML(t *testing.T, fakeClient client.Client) map[string]interface{} {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm); err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}
	raw, ok := cm.Data["maintenance.yaml"]
	if !ok {
		t.Fatal("maintenance.yaml key not found in ConfigMap")
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("maintenance.yaml is not valid YAML: %v\ncontent:\n%s", err, raw)
	}
	return out
}

// TestGatusMaintenanceReconciler_WritesMaintenanceYAML verifies that a single
// GatusMaintenance CR is marshaled and written to maintenance.yaml.
func TestGatusMaintenanceReconciler_WritesMaintenanceYAML(t *testing.T) {
	s := newTestScheme(t)
	m := &monitoringv1alpha1.GatusMaintenance{
		ObjectMeta: metav1.ObjectMeta{Name: "weekly-window", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusMaintenanceSpec{
			Enabled:  true,
			Start:    "02:00",
			Duration: "1h",
			Timezone: "Europe/Paris",
			Every:    []string{"Monday", "Thursday"},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(maintenanceConfigMap(), m).Build()
	r := newMaintenanceReconciler(fakeClient)
	reconcileMaintenance(t, r, "weekly-window", "default")

	out := getMaintenanceYAML(t, fakeClient)
	maint, ok := out["maintenance"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected maintenance map, got: %v", out["maintenance"])
	}
	if maint["start"] != "02:00" {
		t.Errorf("start = %v, want 02:00", maint["start"])
	}
	if maint["duration"] != "1h" {
		t.Errorf("duration = %v, want 1h", maint["duration"])
	}
	if maint["timezone"] != "Europe/Paris" {
		t.Errorf("timezone = %v, want Europe/Paris", maint["timezone"])
	}
	every, ok := maint["every"].([]interface{})
	if !ok || len(every) != 2 {
		t.Errorf("expected every=[Monday, Thursday], got: %v", maint["every"])
	}
}

// TestGatusMaintenanceReconciler_NoMaintenanceCRsWritesEmpty verifies that when no
// GatusMaintenance CRs exist, maintenance.yaml is written as `{}\n`.
func TestGatusMaintenanceReconciler_NoMaintenanceCRsWritesEmpty(t *testing.T) {
	s := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(maintenanceConfigMap()).Build()
	r := newMaintenanceReconciler(fakeClient)
	reconcileMaintenance(t, r, "anything", "default")

	cm := &corev1.ConfigMap{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm)
	raw, ok := cm.Data["maintenance.yaml"]
	if !ok {
		t.Fatal("maintenance.yaml key not found in ConfigMap")
	}
	// Should be valid YAML with no maintenance block.
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("maintenance.yaml is not valid YAML: %v", err)
	}
	if m, exists := out["maintenance"]; exists && m != nil {
		t.Errorf("expected no maintenance block when no CRs exist, got: %v", m)
	}
}

// TestGatusMaintenanceReconciler_MultipleSelectsFirstAlphabetically verifies that when
// multiple GatusMaintenance CRs exist, only the first alphabetically by namespace/name is used.
func TestGatusMaintenanceReconciler_MultipleSelectsFirstAlphabetically(t *testing.T) {
	s := newTestScheme(t)
	first := &monitoringv1alpha1.GatusMaintenance{
		ObjectMeta: metav1.ObjectMeta{Name: "aaa-window", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusMaintenanceSpec{
			Start:    "01:00",
			Duration: "30m",
		},
	}
	second := &monitoringv1alpha1.GatusMaintenance{
		ObjectMeta: metav1.ObjectMeta{Name: "zzz-window", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusMaintenanceSpec{
			Start:    "23:00",
			Duration: "2h",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(maintenanceConfigMap(), first, second).Build()
	r := newMaintenanceReconciler(fakeClient)
	reconcileMaintenance(t, r, "aaa-window", "default")

	out := getMaintenanceYAML(t, fakeClient)
	maint := out["maintenance"].(map[string]interface{})
	if maint["start"] != "01:00" {
		t.Errorf("expected 'aaa-window' (start=01:00) to win, got start=%v", maint["start"])
	}
	if maint["duration"] != "30m" {
		t.Errorf("expected 'aaa-window' (duration=30m) to win, got duration=%v", maint["duration"])
	}
}

// TestGatusMaintenanceReconciler_RequeuesWhenConfigMapMissing verifies that reconciliation
// is requeued (not errored) when the target ConfigMap does not exist.
func TestGatusMaintenanceReconciler_RequeuesWhenConfigMapMissing(t *testing.T) {
	s := newTestScheme(t)
	m := &monitoringv1alpha1.GatusMaintenance{
		ObjectMeta: metav1.ObjectMeta{Name: "my-window", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusMaintenanceSpec{Start: "02:00", Duration: "1h"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(m).Build()
	r := newMaintenanceReconciler(fakeClient)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-window", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile must not return an error when ConfigMap is missing, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when ConfigMap is missing")
	}
}
