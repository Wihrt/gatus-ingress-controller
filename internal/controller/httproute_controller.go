package controller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

const (
	httpRouteLabel = "gatus.io/httproute"
)

// HTTPRouteReconciler reconciles HTTPRoute resources from the Kubernetes Gateway API.
// It creates, updates, and deletes GatusEndpoint resources based on the HTTPRoute spec.
type HTTPRouteReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	GatewayNames []string // watched gateway names; empty means watch all
}

func (r *HTTPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling HTTPRoute", "name", req.Name, "namespace", req.Namespace)

	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, req.NamespacedName, route); err != nil {
		if errors.IsNotFound(err) {
			return r.deleteEndpointsForHTTPRoute(ctx, req.Namespace, req.Name)
		}
		return ctrl.Result{}, fmt.Errorf("failed to get HTTPRoute: %w", err)
	}

	if !r.isWatchedGateway(route) {
		return ctrl.Result{}, nil
	}

	if val, ok := route.Annotations[annotationEnabled]; ok && val == "false" {
		return r.deleteEndpointsForHTTPRoute(ctx, route.Namespace, route.Name)
	}

	group := defaultGroup
	if g, ok := route.Annotations[annotationGroup]; ok && g != "" {
		group = g
	}

	var alertRefs []monitoringv1alpha1.GatusAlertRef
	if alertsStr, ok := route.Annotations[annotationAlerts]; ok && alertsStr != "" {
		for _, alertName := range strings.Split(alertsStr, ",") {
			alertName = strings.TrimSpace(alertName)
			if alertName != "" {
				alertRefs = append(alertRefs, monitoringv1alpha1.GatusAlertRef{
					Name:      alertName,
					Namespace: route.Namespace,
				})
			}
		}
	}

	for _, hostname := range route.Spec.Hostnames {
		host := string(hostname)
		if host == "" {
			continue
		}

		endpointName := fmt.Sprintf("%s-%s", route.Name, sanitizeHostname(host))
		endpoint := &monitoringv1alpha1.GatusEndpoint{}
		err := r.Get(ctx, client.ObjectKey{Namespace: route.Namespace, Name: endpointName}, endpoint)

		desiredSpec := monitoringv1alpha1.GatusEndpointSpec{
			Name:       route.Name,
			Group:      group,
			URL:        fmt.Sprintf("https://%s", host),
			Conditions: []string{"[STATUS] == 200"},
			Alerts:     alertRefs,
		}

		if errors.IsNotFound(err) {
			newEndpoint := &monitoringv1alpha1.GatusEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      endpointName,
					Namespace: route.Namespace,
					Labels: map[string]string{
						managedByLabel: "gatus-ingress-controller",
						httpRouteLabel: route.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: route.APIVersion,
							Kind:       route.Kind,
							Name:       route.Name,
							UID:        route.UID,
						},
					},
				},
				Spec: desiredSpec,
			}
			if err := r.Create(ctx, newEndpoint); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create GatusEndpoint: %w", err)
			}
			logger.Info("Created GatusEndpoint from HTTPRoute", "name", endpointName)
		} else if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get GatusEndpoint: %w", err)
		} else {
			if len(endpoint.OwnerReferences) == 0 {
				logger.Info("GatusEndpoint exists without ownerReferences — user-managed, skipping update", "name", endpointName)
				continue
			}
			endpoint.Spec = desiredSpec
			if err := r.Update(ctx, endpoint); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update GatusEndpoint: %w", err)
			}
			logger.Info("Updated GatusEndpoint from HTTPRoute", "name", endpointName)
		}
	}

	return ctrl.Result{}, nil
}

func (r *HTTPRouteReconciler) deleteEndpointsForHTTPRoute(ctx context.Context, namespace, routeName string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	endpointList := &monitoringv1alpha1.GatusEndpointList{}
	if err := r.List(ctx, endpointList, client.InNamespace(namespace), client.MatchingLabels{httpRouteLabel: routeName}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusEndpoints: %w", err)
	}
	for _, ep := range endpointList.Items {
		if err := r.Delete(ctx, &ep); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to delete GatusEndpoint: %w", err)
		}
		logger.Info("Deleted GatusEndpoint", "name", ep.Name)
	}
	return ctrl.Result{}, nil
}

// isWatchedGateway returns true when the HTTPRoute references at least one of the watched
// gateway names (or when no specific gateway names are configured, meaning watch all).
// Each entry in GatewayNames may be a plain name (e.g. "kong") or a "namespace/name" pair
// (e.g. "infra/kong"). A plain name matches any gateway with that name regardless of namespace.
func (r *HTTPRouteReconciler) isWatchedGateway(route *gatewayv1.HTTPRoute) bool {
	if len(r.GatewayNames) == 0 {
		return true
	}
	for _, ref := range route.Spec.ParentRefs {
		// Resolve the effective namespace: ParentRef.Namespace defaults to the HTTPRoute's namespace.
		refNS := route.Namespace
		if ref.Namespace != nil && string(*ref.Namespace) != "" {
			refNS = string(*ref.Namespace)
		}
		for _, watchedName := range r.GatewayNames {
			if strings.Contains(watchedName, "/") {
				// "namespace/name" format – match both.
				parts := strings.SplitN(watchedName, "/", 2)
				if parts[0] == refNS && parts[1] == string(ref.Name) {
					return true
				}
			} else {
				// Plain name – match by name only.
				if watchedName == string(ref.Name) {
					return true
				}
			}
		}
	}
	return false
}

func (r *HTTPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}).
		Complete(r)
}
