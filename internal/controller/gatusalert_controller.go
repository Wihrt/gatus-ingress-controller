package controller

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

const alertingKey = "alerting.yaml"

// GatusAlertReconciler aggregates all GatusAlert CRs and writes alerting.yaml
// to the shared Gatus ConfigMap. One CR per alert provider type is supported;
// if multiple CRs share the same type, the first (alphabetically by namespace/name) is used.
type GatusAlertReconciler struct {
	client.Client
	TargetNamespace string
	ConfigMapName   string
}

// gatusAlertProviderYAML mirrors the Gatus alerting.<type> config block.
type gatusAlertProviderYAML struct {
	WebhookURL   string                    `yaml:"webhook-url,omitempty"`
	DefaultAlert *gatusDefaultAlertYAML    `yaml:"default-alert,omitempty"`
}

type gatusDefaultAlertYAML struct {
	Enabled                 bool   `yaml:"enabled,omitempty"`
	FailureThreshold        int    `yaml:"failure-threshold,omitempty"`
	SuccessThreshold        int    `yaml:"success-threshold,omitempty"`
	SendOnResolved          bool   `yaml:"send-on-resolved,omitempty"`
	Description             string `yaml:"description,omitempty"`
	MinimumReminderInterval string `yaml:"minimum-reminder-interval,omitempty"`
}

func (r *GatusAlertReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusAlert", "name", req.Name, "namespace", req.Namespace)

	alertList := &monitoringv1alpha1.GatusAlertList{}
	if err := r.List(ctx, alertList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusAlerts: %w", err)
	}

	// Sort by namespace/name for deterministic ordering when deduplicating.
	sort.Slice(alertList.Items, func(i, j int) bool {
		ki := alertList.Items[i].Namespace + "/" + alertList.Items[i].Name
		kj := alertList.Items[j].Namespace + "/" + alertList.Items[j].Name
		return ki < kj
	})

	// Build a map of alert type → provider config. First CR per type wins.
	providers := make(map[string]gatusAlertProviderYAML)
	for _, a := range alertList.Items {
		t := a.Spec.Type
		if t == "" {
			continue
		}
		if _, exists := providers[t]; exists {
			logger.Info("Multiple GatusAlert CRs for the same type — skipping duplicate",
				"type", t, "name", a.Name, "namespace", a.Namespace)
			continue
		}
		providers[t] = gatusAlertProviderYAML{
			WebhookURL: a.Spec.WebhookURL,
			DefaultAlert: &gatusDefaultAlertYAML{
				Enabled:                 a.Spec.Enabled,
				FailureThreshold:        a.Spec.FailureThreshold,
				SuccessThreshold:        a.Spec.SuccessThreshold,
				SendOnResolved:          a.Spec.SendOnResolved,
				Description:             a.Spec.Description,
				MinimumReminderInterval: a.Spec.MinimumReminderInterval,
			},
		}
	}

	cfg := map[string]interface{}{
		"alerting": providers,
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal alerting config: %w", err)
	}

	return upsertConfigMapKey(ctx, r.Client, r.TargetNamespace, r.ConfigMapName, alertingKey, string(data))
}

func (r *GatusAlertReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusAlert{}).
		Complete(r)
}
