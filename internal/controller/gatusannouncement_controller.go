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

const announcementsKey = "announcements.yaml"

// GatusAnnouncementReconciler aggregates all GatusAnnouncement CRs and writes
// announcements.yaml to the shared Gatus ConfigMap.
type GatusAnnouncementReconciler struct {
	client.Client
	TargetNamespace string
	ConfigMapName   string
}

type gatusAnnouncementYAML struct {
	Timestamp string `yaml:"timestamp"`
	Type      string `yaml:"type"`
	Message   string `yaml:"message"`
	Archived  bool   `yaml:"archived,omitempty"`
}

type gatusAnnouncementsFile struct {
	Announcements []gatusAnnouncementYAML `yaml:"announcements"`
}

func (r *GatusAnnouncementReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusAnnouncement", "name", req.Name, "namespace", req.Namespace)

	list := &monitoringv1alpha1.GatusAnnouncementList{}
	if err := r.List(ctx, list); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusAnnouncements: %w", err)
	}

	// Sort newest first (lexicographic on RFC3339 timestamp = chronological order).
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Spec.Timestamp > list.Items[j].Spec.Timestamp
	})

	var announcements []gatusAnnouncementYAML
	for _, a := range list.Items {
		announcements = append(announcements, gatusAnnouncementYAML{
			Timestamp: a.Spec.Timestamp,
			Type:      a.Spec.Type,
			Message:   a.Spec.Message,
			Archived:  a.Spec.Archived,
		})
	}

	cfg := gatusAnnouncementsFile{Announcements: announcements}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal announcements config: %w", err)
	}

	return upsertConfigMapKey(ctx, r.Client, r.TargetNamespace, r.ConfigMapName, announcementsKey, string(data))
}

func (r *GatusAnnouncementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusAnnouncement{}).
		Complete(r)
}
