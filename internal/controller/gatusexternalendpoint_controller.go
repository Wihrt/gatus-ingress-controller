package controller

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/Wihrt/gatus-controller/api/v1alpha1"
)

const externalEndpointsKey = "external-endpoints.yaml"

// GatusExternalEndpointReconciler reconciles GatusExternalEndpoint resources and
// aggregates them into the gatus Secret under the external-endpoints.yaml key.
type GatusExternalEndpointReconciler struct {
	client.Client
	TargetNamespace string
	SecretName      string
}

// --- Internal YAML representation for external endpoints ---

type gatusExternalConfigFile struct {
	ExternalEndpoints []gatusExternalEndpointYAML `yaml:"external-endpoints"`
}

type gatusExternalEndpointYAML struct {
	Name      string              `yaml:"name"`
	Enabled   *bool               `yaml:"enabled,omitempty"`
	Group     string              `yaml:"group,omitempty"`
	Token     string              `yaml:"token"`
	Alerts    []gatusAlertYAML    `yaml:"alerts,omitempty"`
	Heartbeat *gatusHeartbeatYAML `yaml:"heartbeat,omitempty"`
}

type gatusHeartbeatYAML struct {
	Interval string `yaml:"interval,omitempty"`
}

// Reconcile aggregates all GatusExternalEndpoints and writes external-endpoints.yaml
// to the shared gatus Secret.
func (r *GatusExternalEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusExternalEndpoint", "name", req.Name, "namespace", req.Namespace)

	// List all GatusExternalEndpoints across all namespaces.
	extList := &monitoringv1alpha1.GatusExternalEndpointList{}
	if err := r.List(ctx, extList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusExternalEndpoints: %w", err)
	}

	// Sort for deterministic output; first alphabetically (namespace/name) wins.
	sort.Slice(extList.Items, func(i, j int) bool {
		ki := extList.Items[i].Namespace + "/" + extList.Items[i].Name
		kj := extList.Items[j].Namespace + "/" + extList.Items[j].Name
		return ki < kj
	})

	var externalEndpoints []gatusExternalEndpointYAML
	for _, ext := range extList.Items {
		alertYAMLs := convertAlerts(ext.Spec.Alerts)

		extYAML := gatusExternalEndpointYAML{
			Name:    ext.Spec.Name,
			Enabled: boolPtr(ext.Spec.Enabled),
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

	return upsertSecretKey(ctx, r.Client, r.TargetNamespace, r.SecretName, externalEndpointsKey, string(data))
}

func (r *GatusExternalEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusExternalEndpoint{}).
		Complete(r)
}
