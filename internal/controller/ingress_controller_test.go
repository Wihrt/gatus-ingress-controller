package controller

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	if err := monitoringv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add monitoring scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add networking scheme: %v", err)
	}
	return s
}

func TestSanitizeHostname(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com", "example-com"},
		{"foo.bar.baz", "foo-bar-baz"},
		{"*.example.com", "wildcard-example-com"},
		{"plain", "plain"},
	}
	for _, tc := range tests {
		got := sanitizeHostname(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeHostname(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsWatchedClass(t *testing.T) {
	r := &IngressReconciler{IngressClass: "traefik"}

	className := "traefik"
	otherClass := "nginx"

	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected bool
	}{
		{
			name: "matching IngressClassName",
			ingress: &networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{IngressClassName: &className},
			},
			expected: true,
		},
		{
			name: "non-matching IngressClassName",
			ingress: &networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{IngressClassName: &otherClass},
			},
			expected: false,
		},
		{
			name: "matching annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"kubernetes.io/ingress.class": "traefik"},
				},
			},
			expected: true,
		},
		{
			name: "non-matching annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"kubernetes.io/ingress.class": "nginx"},
				},
			},
			expected: false,
		},
		{
			name:     "no class info",
			ingress:  &networkingv1.Ingress{},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := r.isWatchedClass(tc.ingress)
			if got != tc.expected {
				t.Errorf("isWatchedClass() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestIngressReconciler_CreatesEndpoint(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	className := "traefik"
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{
				{Host: "app.example.com"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress).
		Build()

	r := &IngressReconciler{
		Client:       fakeClient,
		Scheme:       scheme,
		IngressClass: "traefik",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ingress", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpoint := &monitoringv1alpha1.GatusEndpoint{}
	endpointName := "my-ingress-app-example-com"
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: endpointName, Namespace: "default"}, endpoint); err != nil {
		t.Fatalf("GatusEndpoint not found: %v", err)
	}

	if endpoint.Spec.URL != "https://app.example.com" {
		t.Errorf("GatusEndpoint URL = %q, want %q", endpoint.Spec.URL, "https://app.example.com")
	}
	if endpoint.Spec.Group != defaultGroup {
		t.Errorf("GatusEndpoint Group = %q, want %q", endpoint.Spec.Group, defaultGroup)
	}
}

func TestIngressReconciler_UpdatesExistingEndpoint(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	className := "traefik"
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress",
			Namespace: "default",
			Annotations: map[string]string{
				annotationGroup: "production",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{
				{Host: "app.example.com"},
			},
		},
	}

	existingEndpoint := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress-app-example-com",
			Namespace: "default",
			Labels: map[string]string{
				managedByLabel: "gatus-ingress-controller",
				ingressLabel:   "my-ingress",
			},
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "networking.k8s.io/v1", Kind: "Ingress", Name: "my-ingress"},
			},
		},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name:  "my-ingress",
			Group: "old-group",
			URL:   "https://app.example.com",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress, existingEndpoint).
		Build()

	r := &IngressReconciler{
		Client:       fakeClient,
		Scheme:       scheme,
		IngressClass: "traefik",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ingress", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpoint := &monitoringv1alpha1.GatusEndpoint{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "my-ingress-app-example-com", Namespace: "default"}, endpoint); err != nil {
		t.Fatalf("GatusEndpoint not found: %v", err)
	}

	if endpoint.Spec.Group != "production" {
		t.Errorf("GatusEndpoint Group = %q, want %q", endpoint.Spec.Group, "production")
	}
}

func TestIngressReconciler_DeletesEndpointWhenDisabled(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	className := "traefik"
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress",
			Namespace: "default",
			Annotations: map[string]string{
				annotationEnabled: "false",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
		},
	}

	existingEndpoint := &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress-app-example-com",
			Namespace: "default",
			Labels: map[string]string{
				ingressLabel: "my-ingress",
			},
		},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name: "my-ingress",
			URL:  "https://app.example.com",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress, existingEndpoint).
		Build()

	r := &IngressReconciler{
		Client:       fakeClient,
		Scheme:       scheme,
		IngressClass: "traefik",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ingress", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpointList := &monitoringv1alpha1.GatusEndpointList{}
	if err := fakeClient.List(ctx, endpointList); err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(endpointList.Items) != 0 {
		t.Errorf("expected 0 GatusEndpoints after disable, got %d", len(endpointList.Items))
	}
}

func TestIngressReconciler_IgnoresWrongClass(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	className := "nginx"
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{
				{Host: "app.example.com"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress).
		Build()

	r := &IngressReconciler{
		Client:       fakeClient,
		Scheme:       scheme,
		IngressClass: "traefik",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ingress", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpointList := &monitoringv1alpha1.GatusEndpointList{}
	if err := fakeClient.List(ctx, endpointList); err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(endpointList.Items) != 0 {
		t.Errorf("expected 0 GatusEndpoints for wrong ingress class, got %d", len(endpointList.Items))
	}
}

func TestIngressReconciler_ParsesAlertsAnnotation(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	className := "traefik"
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress",
			Namespace: "default",
			Annotations: map[string]string{
				annotationAlerts: "alert-slack, alert-email",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{
				{Host: "app.example.com"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress).
		Build()

	r := &IngressReconciler{
		Client:       fakeClient,
		Scheme:       scheme,
		IngressClass: "traefik",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ingress", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpoint := &monitoringv1alpha1.GatusEndpoint{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "my-ingress-app-example-com", Namespace: "default"}, endpoint); err != nil {
		t.Fatalf("GatusEndpoint not found: %v", err)
	}

	if len(endpoint.Spec.Alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(endpoint.Spec.Alerts))
	}
	if endpoint.Spec.Alerts[0].Name != "alert-slack" {
		t.Errorf("alert[0].Name = %q, want %q", endpoint.Spec.Alerts[0].Name, "alert-slack")
	}
	if endpoint.Spec.Alerts[1].Name != "alert-email" {
		t.Errorf("alert[1].Name = %q, want %q", endpoint.Spec.Alerts[1].Name, "alert-email")
	}
}

func TestIngressReconciler_SkipsUserManagedEndpoint(t *testing.T) {
ctx := context.Background()
scheme := newTestScheme(t)

className := "traefik"
ingress := &networkingv1.Ingress{
ObjectMeta: metav1.ObjectMeta{
Name:      "my-ingress",
Namespace: "default",
Annotations: map[string]string{
annotationGroup: "from-ingress",
},
},
Spec: networkingv1.IngressSpec{
IngressClassName: &className,
Rules: []networkingv1.IngressRule{
{Host: "app.example.com"},
},
},
}

// User-created CR: same name as what the controller would generate, but NO ownerReferences.
userEndpoint := &monitoringv1alpha1.GatusEndpoint{
ObjectMeta: metav1.ObjectMeta{
Name:      "my-ingress-app-example-com",
Namespace: "default",
// No OwnerReferences — marks this as user-managed.
},
Spec: monitoringv1alpha1.GatusEndpointSpec{
Name:  "custom-name",
Group: "user-defined-group",
URL:   "https://custom.example.com",
},
}

fakeClient := fake.NewClientBuilder().
WithScheme(scheme).
WithObjects(ingress, userEndpoint).
Build()

r := &IngressReconciler{
Client:       fakeClient,
Scheme:       scheme,
IngressClass: "traefik",
}

req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-ingress", Namespace: "default"}}
_, err := r.Reconcile(ctx, req)
if err != nil {
t.Fatalf("Reconcile returned error: %v", err)
}

endpoint := &monitoringv1alpha1.GatusEndpoint{}
if err := fakeClient.Get(ctx, types.NamespacedName{Name: "my-ingress-app-example-com", Namespace: "default"}, endpoint); err != nil {
t.Fatalf("GatusEndpoint not found: %v", err)
}

// The user's spec must be preserved — the controller must NOT have overwritten it.
if endpoint.Spec.Group != "user-defined-group" {
t.Errorf("GatusEndpoint Group = %q, want %q (user-managed CR must not be overwritten)", endpoint.Spec.Group, "user-defined-group")
}
if endpoint.Spec.URL != "https://custom.example.com" {
t.Errorf("GatusEndpoint URL = %q, want %q (user-managed CR must not be overwritten)", endpoint.Spec.URL, "https://custom.example.com")
}
}
