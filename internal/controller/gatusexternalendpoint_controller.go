package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"gopkg.in/yaml.v3"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

const externalEndpointsKey = "external-endpoints.yaml"

// GatusExternalEndpointReconciler reconciles GatusExternalEndpoint resources and
// aggregates them into the gatus-config ConfigMap under the external-endpoints.yaml key.
type GatusExternalEndpointReconciler struct {
	client.Client
	TargetNamespace string
	ConfigMapName   string
}

// --- Internal YAML representation for external endpoints ---

type gatusExternalConfigFile struct {
	ExternalEndpoints []gatusExternalEndpointYAML `yaml:"external-endpoints"`
}

type gatusExternalEndpointYAML struct {
	Name      string           `yaml:"name"`
	Enabled   bool             `yaml:"enabled,omitempty"`
	Group     string           `yaml:"group,omitempty"`
	Token     string           `yaml:"token"`
	Alerts    []gatusAlertYAML `yaml:"alerts,omitempty"`
	Heartbeat *gatusHeartbeatYAML `yaml:"heartbeat,omitempty"`
}

type gatusHeartbeatYAML struct {
	Interval string `yaml:"interval,omitempty"`
}

// Reconcile aggregates all GatusExternalEndpoints and writes external-endpoints.yaml
// to the shared gatus-config ConfigMap.
func (r *GatusExternalEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusExternalEndpoint", "name", req.Name, "namespace", req.Namespace)

	// List all GatusExternalEndpoints across all namespaces.
	extList := &monitoringv1alpha1.GatusExternalEndpointList{}
	if err := r.List(ctx, extList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusExternalEndpoints: %w", err)
	}

	var externalEndpoints []gatusExternalEndpointYAML
	for _, ext := range extList.Items {
		alertYAMLs, err := r.resolveExtAlerts(ctx, ext.Spec.Alerts, ext.Namespace)
		if err != nil {
			logger.Error(err, "Failed to resolve alerts for external endpoint", "name", ext.Name)
		}

		extYAML := gatusExternalEndpointYAML{
			Name:    ext.Spec.Name,
			Enabled: ext.Spec.Enabled,
			Group:   ext.Spec.Group,
			Token:   ext.Spec.Token,
			Alerts:  alertYAMLs,
		}

		if ext.Spec.Heartbeat != nil && ext.Spec.Heartbeat.Interval != "" {
			extYAML.Heartbeat = &gatusHeartbeatYAML{
				Interval: ext.Spec.Heartbeat.Interval,
			}
		}

		externalEndpoints = append(externalEndpoints, extYAML)
	}

	cfg := gatusExternalConfigFile{ExternalEndpoints: externalEndpoints}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal Gatus external endpoints config: %w", err)
	}

	return upsertConfigMapKey(ctx, r.Client, r.TargetNamespace, r.ConfigMapName, externalEndpointsKey, string(data))
}

// resolveExtAlerts resolves GatusAlertRefs to YAML alert configs for external endpoints.
func (r *GatusExternalEndpointReconciler) resolveExtAlerts(ctx context.Context, refs []monitoringv1alpha1.GatusAlertRef, defaultNS string) ([]gatusAlertYAML, error) {
	logger := log.FromContext(ctx)
	var out []gatusAlertYAML
	for _, ref := range refs {
		ns := ref.Namespace
		if ns == "" {
			ns = defaultNS
		}
		alert := &monitoringv1alpha1.GatusAlert{}
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, alert); err != nil {
			logger.Error(err, "Failed to get GatusAlert", "name", ref.Name, "namespace", ns)
			continue
		}

		y := gatusAlertYAML{
			Type:                    alert.Spec.Type,
			Enabled:                 alert.Spec.Enabled,
			Description:             alert.Spec.Description,
			FailureThreshold:        alert.Spec.FailureThreshold,
			SuccessThreshold:        alert.Spec.SuccessThreshold,
			SendOnResolved:          alert.Spec.SendOnResolved,
			MinimumReminderInterval: alert.Spec.MinimumReminderInterval,
		}

		// Apply per-endpoint overrides.
		if ref.Description != "" {
			y.Description = ref.Description
		}
		if ref.Enabled != nil {
			y.Enabled = *ref.Enabled
		}
		if ref.FailureThreshold != 0 {
			y.FailureThreshold = ref.FailureThreshold
		}
		if ref.SuccessThreshold != 0 {
			y.SuccessThreshold = ref.SuccessThreshold
		}
		if ref.SendOnResolved != nil {
			y.SendOnResolved = *ref.SendOnResolved
		}
		if ref.MinimumReminderInterval != "" {
			y.MinimumReminderInterval = ref.MinimumReminderInterval
		}

		out = append(out, y)
	}
	return out, nil
}

func (r *GatusExternalEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusExternalEndpoint{}).
		Complete(r)
}
