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

const maintenanceKey = "maintenance.yaml"

// GatusMaintenanceReconciler aggregates GatusMaintenance CRs and writes maintenance.yaml
// to the shared Gatus ConfigMap. Gatus only supports a single global maintenance block,
// so only the first CR (alphabetically by namespace/name) is used.
type GatusMaintenanceReconciler struct {
	client.Client
	TargetNamespace string
	ConfigMapName   string
}

type gatusMaintenanceFileYAML struct {
	Maintenance *gatusMaintenanceGlobalYAML `yaml:"maintenance,omitempty"`
}

type gatusMaintenanceGlobalYAML struct {
	Enabled  bool     `yaml:"enabled,omitempty"`
	Start    string   `yaml:"start"`
	Duration string   `yaml:"duration"`
	Timezone string   `yaml:"timezone,omitempty"`
	Every    []string `yaml:"every,omitempty"`
}

func (r *GatusMaintenanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusMaintenance", "name", req.Name, "namespace", req.Namespace)

	list := &monitoringv1alpha1.GatusMaintenanceList{}
	if err := r.List(ctx, list); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusMaintenances: %w", err)
	}

	if len(list.Items) == 0 {
		// No maintenance CRs — write an empty file.
		return upsertConfigMapKey(ctx, r.Client, r.TargetNamespace, r.ConfigMapName, maintenanceKey, "{}\n")
	}

	// Sort by namespace/name for deterministic selection.
	sort.Slice(list.Items, func(i, j int) bool {
		ki := list.Items[i].Namespace + "/" + list.Items[i].Name
		kj := list.Items[j].Namespace + "/" + list.Items[j].Name
		return ki < kj
	})

	if len(list.Items) > 1 {
		logger.Info("Multiple GatusMaintenance CRs found — only the first (alphabetically) will be used",
			"selected", list.Items[0].Namespace+"/"+list.Items[0].Name)
	}

	m := list.Items[0].Spec
	cfg := gatusMaintenanceFileYAML{
		Maintenance: &gatusMaintenanceGlobalYAML{
			Enabled:  m.Enabled,
			Start:    m.Start,
			Duration: m.Duration,
			Timezone: m.Timezone,
			Every:    m.Every,
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal maintenance config: %w", err)
	}

	return upsertConfigMapKey(ctx, r.Client, r.TargetNamespace, r.ConfigMapName, maintenanceKey, string(data))
}

func (r *GatusMaintenanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusMaintenance{}).
		Complete(r)
}
