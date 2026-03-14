package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpsertSecretKey_CreatesKeyInExistingSecret(t *testing.T) {
	s := newTestScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "test-ns"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()

	result, err := upsertSecretKey(context.Background(), fakeClient, "test-ns", "test-secret", "my-key", "my-value")
	if err != nil {
		t.Fatalf("upsertSecretKey returned unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got RequeueAfter=%v", result.RequeueAfter)
	}

	updated := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-secret", Namespace: "test-ns"}, updated); err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}
	if string(updated.Data["my-key"]) != "my-value" {
		t.Errorf("expected 'my-value', got %q", string(updated.Data["my-key"]))
	}
}

func TestUpsertSecretKey_UpdatesExistingKey(t *testing.T) {
	s := newTestScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "test-ns"},
		Data:       map[string][]byte{"my-key": []byte("old-value")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()

	_, err := upsertSecretKey(context.Background(), fakeClient, "test-ns", "test-secret", "my-key", "new-value")
	if err != nil {
		t.Fatalf("upsertSecretKey returned unexpected error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-secret", Namespace: "test-ns"}, updated)
	if string(updated.Data["my-key"]) != "new-value" {
		t.Errorf("expected 'new-value', got %q", string(updated.Data["my-key"]))
	}
}

func TestUpsertSecretKey_PreservesOtherKeys(t *testing.T) {
	s := newTestScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "test-ns"},
		Data:       map[string][]byte{"existing-key": []byte("existing-value")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()

	_, err := upsertSecretKey(context.Background(), fakeClient, "test-ns", "test-secret", "new-key", "new-value")
	if err != nil {
		t.Fatalf("upsertSecretKey returned unexpected error: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-secret", Namespace: "test-ns"}, updated)
	if string(updated.Data["existing-key"]) != "existing-value" {
		t.Errorf("existing key was modified: got %q", string(updated.Data["existing-key"]))
	}
	if string(updated.Data["new-key"]) != "new-value" {
		t.Errorf("new key not set: got %q", string(updated.Data["new-key"]))
	}
}

func TestUpsertSecretKey_RequeuesWhenSecretNotFound(t *testing.T) {
	s := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	result, err := upsertSecretKey(context.Background(), fakeClient, "test-ns", "missing-secret", "key", "value")
	if err != nil {
		t.Fatalf("upsertSecretKey should not return error for missing Secret, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when Secret is missing")
	}
}

func TestUpsertSecretKey_HandlesNilDataMap(t *testing.T) {
	s := newTestScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "test-ns"},
		// Data is nil
	}
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()

	_, err := upsertSecretKey(context.Background(), fakeClient, "test-ns", "test-secret", "key", "value")
	if err != nil {
		t.Fatalf("upsertSecretKey should handle nil Data map, got: %v", err)
	}

	updated := &corev1.Secret{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-secret", Namespace: "test-ns"}, updated)
	if string(updated.Data["key"]) != "value" {
		t.Errorf("expected 'value', got %q", string(updated.Data["key"]))
	}
}
