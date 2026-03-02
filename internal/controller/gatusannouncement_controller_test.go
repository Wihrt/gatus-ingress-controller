package controller

import (
	"context"
	"strings"
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

func newAnnouncementReconciler(fakeClient client.Client) *GatusAnnouncementReconciler {
	return &GatusAnnouncementReconciler{
		Client:          fakeClient,
		TargetNamespace: "gatus",
		ConfigMapName:   "gatus-config",
	}
}

func announcementConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "gatus-config", Namespace: "gatus"},
		Data:       map[string]string{"config.yaml": "web:\n  port: 8080\n"},
	}
}

func reconcileAnnouncement(t *testing.T, r *GatusAnnouncementReconciler, name, namespace string) ctrl.Result {
	t.Helper()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	if err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}
	return result
}

func getAnnouncementsYAML(t *testing.T, fakeClient client.Client) map[string]interface{} {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm); err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}
	raw, ok := cm.Data["announcements.yaml"]
	if !ok {
		t.Fatal("announcements.yaml key not found in ConfigMap")
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("announcements.yaml is not valid YAML: %v\ncontent:\n%s", err, raw)
	}
	return out
}

// TestGatusAnnouncementReconciler_WritesAnnouncementsYAML verifies a single
// GatusAnnouncement CR is marshaled into announcements.yaml.
func TestGatusAnnouncementReconciler_WritesAnnouncementsYAML(t *testing.T) {
	s := newTestScheme(t)
	ann := &monitoringv1alpha1.GatusAnnouncement{
		ObjectMeta: metav1.ObjectMeta{Name: "outage-2025", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAnnouncementSpec{
			Timestamp: "2025-06-01T10:00:00Z",
			Type:      "outage",
			Message:   "Scheduled maintenance on payment service",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(announcementConfigMap(), ann).Build()
	r := newAnnouncementReconciler(fakeClient)
	reconcileAnnouncement(t, r, "outage-2025", "default")

	out := getAnnouncementsYAML(t, fakeClient)
	anns, ok := out["announcements"].([]interface{})
	if !ok || len(anns) == 0 {
		t.Fatalf("expected non-empty announcements list, got: %v", out["announcements"])
	}
	entry := anns[0].(map[string]interface{})
	if entry["timestamp"] != "2025-06-01T10:00:00Z" {
		t.Errorf("timestamp = %v, want 2025-06-01T10:00:00Z", entry["timestamp"])
	}
	if entry["type"] != "outage" {
		t.Errorf("type = %v, want outage", entry["type"])
	}
	if entry["message"] != "Scheduled maintenance on payment service" {
		t.Errorf("message = %v, want the expected string", entry["message"])
	}
}

// TestGatusAnnouncementReconciler_MultipleAnnouncementsSortedNewestFirst verifies
// that announcements are sorted with the newest timestamp first.
func TestGatusAnnouncementReconciler_MultipleAnnouncementsSortedNewestFirst(t *testing.T) {
	s := newTestScheme(t)
	older := &monitoringv1alpha1.GatusAnnouncement{
		ObjectMeta: metav1.ObjectMeta{Name: "older-ann", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAnnouncementSpec{
			Timestamp: "2025-01-01T00:00:00Z",
			Type:      "information",
			Message:   "Older announcement",
		},
	}
	newer := &monitoringv1alpha1.GatusAnnouncement{
		ObjectMeta: metav1.ObjectMeta{Name: "newer-ann", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAnnouncementSpec{
			Timestamp: "2025-06-01T00:00:00Z",
			Type:      "warning",
			Message:   "Newer announcement",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(announcementConfigMap(), older, newer).Build()
	r := newAnnouncementReconciler(fakeClient)
	reconcileAnnouncement(t, r, "newer-ann", "default")

	out := getAnnouncementsYAML(t, fakeClient)
	anns := out["announcements"].([]interface{})
	if len(anns) != 2 {
		t.Fatalf("expected 2 announcements, got %d", len(anns))
	}
	first := anns[0].(map[string]interface{})
	if first["timestamp"] != "2025-06-01T00:00:00Z" {
		t.Errorf("expected newest announcement first, got timestamp=%v", first["timestamp"])
	}
}

// TestGatusAnnouncementReconciler_NoAnnouncementsWritesEmptyList verifies that when
// no CRs exist the key is written with an empty announcements list.
func TestGatusAnnouncementReconciler_NoAnnouncementsWritesEmptyList(t *testing.T) {
	s := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(announcementConfigMap()).Build()
	r := newAnnouncementReconciler(fakeClient)
	reconcileAnnouncement(t, r, "anything", "default")

	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm); err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}
	raw, ok := cm.Data["announcements.yaml"]
	if !ok {
		t.Fatal("announcements.yaml key not found in ConfigMap")
	}
	// Should be valid YAML; announcements key may be null or empty list.
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("announcements.yaml is not valid YAML: %v", err)
	}
	anns := out["announcements"]
	// nil (YAML null), empty slice, or []interface{}{} are all acceptable.
	if anns != nil {
		if list, ok := anns.([]interface{}); ok && len(list) != 0 {
			t.Errorf("expected empty announcements list, got: %v", list)
		}
	}
}

// TestGatusAnnouncementReconciler_SpecialCharactersInMessage verifies that a message
// containing YAML-special characters (colon, quotes, newline) is marshaled into valid YAML.
func TestGatusAnnouncementReconciler_SpecialCharactersInMessage(t *testing.T) {
	s := newTestScheme(t)
	ann := &monitoringv1alpha1.GatusAnnouncement{
		ObjectMeta: metav1.ObjectMeta{Name: "special-chars", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusAnnouncementSpec{
			Timestamp: "2025-06-01T10:00:00Z",
			Type:      "information",
			Message:   `Alert: "database: down" — see https://example.com/status?q=1&r=2`,
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(announcementConfigMap(), ann).Build()
	r := newAnnouncementReconciler(fakeClient)
	reconcileAnnouncement(t, r, "special-chars", "default")

	cm := &corev1.ConfigMap{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "gatus-config", Namespace: "gatus"}, cm)
	raw := cm.Data["announcements.yaml"]

	// Must be valid YAML.
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("announcements.yaml with special chars is not valid YAML: %v\ncontent:\n%s", err, raw)
	}
	// The message must round-trip correctly.
	anns := out["announcements"].([]interface{})
	entry := anns[0].(map[string]interface{})
	want := `Alert: "database: down" — see https://example.com/status?q=1&r=2`
	if entry["message"] != want {
		t.Errorf("message round-trip failed:\n got:  %v\n want: %v", entry["message"], want)
	}
	_ = strings.Contains // avoid unused import error
}

// TestGatusAnnouncementReconciler_RequeuesWhenConfigMapMissing verifies that reconciliation
// is requeued (not errored) when the target ConfigMap does not exist.
func TestGatusAnnouncementReconciler_RequeuesWhenConfigMapMissing(t *testing.T) {
	s := newTestScheme(t)
	ann := &monitoringv1alpha1.GatusAnnouncement{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ann", Namespace: "default"},
		Spec:       monitoringv1alpha1.GatusAnnouncementSpec{Timestamp: "2025-01-01T00:00:00Z", Type: "none", Message: "hi"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(ann).Build()
	r := newAnnouncementReconciler(fakeClient)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-ann", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile must not return an error when ConfigMap is missing, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when ConfigMap is missing")
	}
}
