package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func TestHTTPRouteReconciler_CreatesEndpoint(t *testing.T) {
	ctx := context.Background()

	s := newTestScheme(t)
	if err := gatewayv1.Install(s); err != nil {
		t.Fatalf("failed to add gateway scheme: %v", err)
	}

	gatewayName := gatewayv1.ObjectName("my-gateway")
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-route",
			Namespace: "default",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: gatewayName},
				},
			},
			Hostnames: []gatewayv1.Hostname{"app.example.com"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(route).
		Build()

	r := &HTTPRouteReconciler{
		Client:       fakeClient,
		Scheme:       s,
		GatewayNames: []string{"my-gateway"},
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-route", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpoint := &monitoringv1alpha1.GatusEndpoint{}
	endpointName := "my-route-app-example-com"
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: endpointName, Namespace: "default"}, endpoint); err != nil {
		t.Fatalf("GatusEndpoint not found: %v", err)
	}

	if endpoint.Spec.URL != "https://app.example.com" {
		t.Errorf("GatusEndpoint URL = %q, want %q", endpoint.Spec.URL, "https://app.example.com")
	}
}

func TestHTTPRouteReconciler_WatchesAllGatewaysWhenEmpty(t *testing.T) {
	ctx := context.Background()

	s := newTestScheme(t)
	if err := gatewayv1.Install(s); err != nil {
		t.Fatalf("failed to add gateway scheme: %v", err)
	}

	gatewayName := gatewayv1.ObjectName("any-gateway")
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-route",
			Namespace: "default",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: gatewayName},
				},
			},
			Hostnames: []gatewayv1.Hostname{"app.example.com"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(route).
		Build()

	r := &HTTPRouteReconciler{
		Client:       fakeClient,
		Scheme:       s,
		GatewayNames: []string{}, // empty = watch all
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-route", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpointList := &monitoringv1alpha1.GatusEndpointList{}
	if err := fakeClient.List(ctx, endpointList); err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(endpointList.Items) != 1 {
		t.Errorf("expected 1 GatusEndpoint, got %d", len(endpointList.Items))
	}
}

func TestHTTPRouteReconciler_IgnoresUnwatchedGateway(t *testing.T) {
	ctx := context.Background()

	s := newTestScheme(t)
	if err := gatewayv1.Install(s); err != nil {
		t.Fatalf("failed to add gateway scheme: %v", err)
	}

	gatewayName := gatewayv1.ObjectName("other-gateway")
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-route",
			Namespace: "default",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: gatewayName},
				},
			},
			Hostnames: []gatewayv1.Hostname{"app.example.com"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(route).
		Build()

	r := &HTTPRouteReconciler{
		Client:       fakeClient,
		Scheme:       s,
		GatewayNames: []string{"watched-gateway"},
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-route", Namespace: "default"}}
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	endpointList := &monitoringv1alpha1.GatusEndpointList{}
	if err := fakeClient.List(ctx, endpointList); err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(endpointList.Items) != 0 {
		t.Errorf("expected 0 GatusEndpoints for unwatched gateway, got %d", len(endpointList.Items))
	}
}

func TestIsWatchedGateway(t *testing.T) {
	nsDefault := gatewayv1.Namespace("default")
	nsOther := gatewayv1.Namespace("other")

	makeRoute := func(ns string, parentRefs ...gatewayv1.ParentReference) *gatewayv1.HTTPRoute {
		return &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
			},
		}
	}

	tests := []struct {
		name         string
		gatewayNames []string
		route        *gatewayv1.HTTPRoute
		expected     bool
	}{
		{
			name:         "empty gateway names watches all",
			gatewayNames: []string{},
			route: makeRoute("default", gatewayv1.ParentReference{
				Name: "any-gateway",
			}),
			expected: true,
		},
		{
			name:         "matching plain name",
			gatewayNames: []string{"my-gw"},
			route: makeRoute("default", gatewayv1.ParentReference{
				Name: "my-gw",
			}),
			expected: true,
		},
		{
			name:         "non-matching plain name",
			gatewayNames: []string{"other-gw"},
			route: makeRoute("default", gatewayv1.ParentReference{
				Name: "my-gw",
			}),
			expected: false,
		},
		{
			name:         "matching namespace/name",
			gatewayNames: []string{"default/my-gw"},
			route: makeRoute("default", gatewayv1.ParentReference{
				Name:      "my-gw",
				Namespace: &nsDefault,
			}),
			expected: true,
		},
		{
			name:         "non-matching namespace in namespace/name",
			gatewayNames: []string{"infra/my-gw"},
			route: makeRoute("default", gatewayv1.ParentReference{
				Name:      "my-gw",
				Namespace: &nsOther,
			}),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &HTTPRouteReconciler{GatewayNames: tc.gatewayNames}
			got := r.isWatchedGateway(tc.route)
			if got != tc.expected {
				t.Errorf("isWatchedGateway() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestHTTPRouteReconciler_SkipsUserManagedEndpoint(t *testing.T) {
ctx := context.Background()

s := newTestScheme(t)
if err := gatewayv1.Install(s); err != nil {
t.Fatalf("failed to add gateway scheme: %v", err)
}

gatewayName := gatewayv1.ObjectName("my-gateway")
route := &gatewayv1.HTTPRoute{
ObjectMeta: metav1.ObjectMeta{
Name:      "my-route",
Namespace: "default",
Annotations: map[string]string{
annotationGroup: "from-httproute",
},
},
Spec: gatewayv1.HTTPRouteSpec{
CommonRouteSpec: gatewayv1.CommonRouteSpec{
ParentRefs: []gatewayv1.ParentReference{
{Name: gatewayName},
},
},
Hostnames: []gatewayv1.Hostname{"app.example.com"},
},
}

// User-created CR: same name as what the controller would generate, but NO ownerReferences.
userEndpoint := &monitoringv1alpha1.GatusEndpoint{
ObjectMeta: metav1.ObjectMeta{
Name:      "my-route-app-example-com",
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
WithScheme(s).
WithObjects(route, userEndpoint).
Build()

r := &HTTPRouteReconciler{
Client:       fakeClient,
Scheme:       s,
GatewayNames: []string{"my-gateway"},
}

req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "my-route", Namespace: "default"}}
_, err := r.Reconcile(ctx, req)
if err != nil {
t.Fatalf("Reconcile returned error: %v", err)
}

endpoint := &monitoringv1alpha1.GatusEndpoint{}
if err := fakeClient.Get(ctx, types.NamespacedName{Name: "my-route-app-example-com", Namespace: "default"}, endpoint); err != nil {
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
