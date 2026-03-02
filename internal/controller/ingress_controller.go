package controller

import (
	"context"
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

const (
	annotationEnabled = "gatus.io/enabled"
	annotationGroup   = "gatus.io/group"
	annotationAlerts  = "gatus.io/alerts"

	defaultGroup        = "external"
	defaultIngressClass = "traefik"

	managedByLabel = "gatus.io/managed-by"
	ingressLabel   = "gatus.io/ingress"
)

type IngressReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	IngressClass string
}

func (r *IngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Ingress", "name", req.Name, "namespace", req.Namespace)

	ingress := &networkingv1.Ingress{}
	if err := r.Get(ctx, req.NamespacedName, ingress); err != nil {
		if errors.IsNotFound(err) {
			return r.deleteEndpointsForIngress(ctx, req.Namespace, req.Name)
		}
		return ctrl.Result{}, fmt.Errorf("failed to get Ingress: %w", err)
	}

	if !r.isWatchedClass(ingress) {
		return ctrl.Result{}, nil
	}

	if val, ok := ingress.Annotations[annotationEnabled]; ok && val == "false" {
		return r.deleteEndpointsForIngress(ctx, ingress.Namespace, ingress.Name)
	}

	group := defaultGroup
	if g, ok := ingress.Annotations[annotationGroup]; ok && g != "" {
		group = g
	}

	var alertRefs []monitoringv1alpha1.GatusAlertRef
	if alertsStr, ok := ingress.Annotations[annotationAlerts]; ok && alertsStr != "" {
		for _, alertName := range strings.Split(alertsStr, ",") {
			alertName = strings.TrimSpace(alertName)
			if alertName != "" {
				alertRefs = append(alertRefs, monitoringv1alpha1.GatusAlertRef{
					Name:      alertName,
					Namespace: ingress.Namespace,
				})
			}
		}
	}

	for _, rule := range ingress.Spec.Rules {
		if rule.Host == "" {
			continue
		}

		endpointName := fmt.Sprintf("%s-%s", ingress.Name, sanitizeHostname(rule.Host))
		endpoint := &monitoringv1alpha1.GatusEndpoint{}
		err := r.Get(ctx, client.ObjectKey{Namespace: ingress.Namespace, Name: endpointName}, endpoint)

		desiredSpec := monitoringv1alpha1.GatusEndpointSpec{
			Name:       ingress.Name,
			Group:      group,
			URL:        fmt.Sprintf("https://%s", rule.Host),
			Conditions: []string{"[STATUS] == 200"},
			Alerts:     alertRefs,
		}

		if errors.IsNotFound(err) {
			newEndpoint := &monitoringv1alpha1.GatusEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      endpointName,
					Namespace: ingress.Namespace,
					Labels: map[string]string{
						managedByLabel: "gatus-ingress-controller",
						ingressLabel:   ingress.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: ingress.APIVersion,
							Kind:       ingress.Kind,
							Name:       ingress.Name,
							UID:        ingress.UID,
						},
					},
				},
				Spec: desiredSpec,
			}
			if err := r.Create(ctx, newEndpoint); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create GatusEndpoint: %w", err)
			}
			logger.Info("Created GatusEndpoint", "name", endpointName)
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
			logger.Info("Updated GatusEndpoint", "name", endpointName)
		}
	}

	return ctrl.Result{}, nil
}

func (r *IngressReconciler) deleteEndpointsForIngress(ctx context.Context, namespace, ingressName string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	endpointList := &monitoringv1alpha1.GatusEndpointList{}
	if err := r.List(ctx, endpointList, client.InNamespace(namespace), client.MatchingLabels{ingressLabel: ingressName}); err != nil {
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

func (r *IngressReconciler) isWatchedClass(ingress *networkingv1.Ingress) bool {
	if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == r.IngressClass {
		return true
	}
	if class, ok := ingress.Annotations["kubernetes.io/ingress.class"]; ok && class == r.IngressClass {
		return true
	}
	return false
}

func sanitizeHostname(hostname string) string {
	result := strings.ReplaceAll(hostname, ".", "-")
	result = strings.ReplaceAll(result, "*", "wildcard")
	return result
}

func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Complete(r)
}
