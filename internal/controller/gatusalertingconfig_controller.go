package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

const alertingKey = "alerting.yaml"

// providerRequiredFields lists the required spec.config keys for each provider type.
var providerRequiredFields = map[string][]string{
	"awsses":          {"region", "from", "to"},
	"clickup":         {"list-id", "token"},
	"custom":          {"url"},
	"datadog":         {"api-key", "account-id"},
	"discord":         {"webhook-url"},
	"email":           {"from", "host", "port", "to"},
	"gitea":           {"repository-url", "token"},
	"github":          {"repository-url", "token"},
	"gitlab":          {"webhook-url", "authorization-key"},
	"googlechat":      {"webhook-url"},
	"gotify":          {"server-url", "token"},
	"homeassistant":   {"url", "token"},
	"ifttt":           {"webhook-key", "event-name"},
	"ilert":           {"integration-key"},
	"incident-io":     {"url", "auth-token"},
	"line":            {"channel-access-token", "user-ids"},
	"matrix":          {"access-token", "internal-room-id"},
	"mattermost":      {"webhook-url"},
	"messagebird":     {"access-key", "originator", "recipients"},
	"n8n":             {"webhook-url"},
	"newrelic":        {"api-key", "account-id"},
	"ntfy":            {"topic"},
	"opsgenie":        {"api-key"},
	"pagerduty":       {"integration-key"},
	"plivo":           {"auth-id", "auth-token", "from", "to"},
	"pushover":        {"application-token", "user-key"},
	"rocketchat":      {"webhook-url"},
	"sendgrid":        {"api-key", "from", "to"},
	"signal":          {"cli-client-url"},
	"signl4":          {"token"},
	"slack":           {"webhook-url"},
	"splunk":          {"token", "url"},
	"squadcast":       {"webhook-url"},
	"teams":           {"webhook-url"},
	"teams-workflows": {"webhook-url"},
	"telegram":        {"token", "id"},
	"twilio":          {"sid", "token", "from", "to"},
	"vonage":          {"api-key", "api-secret", "from", "to"},
	"webex":           {"webhook-url"},
	"zapier":          {"webhook-url"},
	"zulip":           {"bot-email", "bot-api-key", "domain", "channel", "topic"},
}

// GatusAlertingConfigReconciler reconciles GatusAlertingConfig resources.
// It validates the config against the required fields for the provider type,
// sets a status condition, then aggregates all valid configs into alerting.yaml in the target Secret.
type GatusAlertingConfigReconciler struct {
	client.Client
	TargetNamespace     string
	SecretName          string
	ControllerNamespace string
}

func (r *GatusAlertingConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusAlertingConfig", "name", req.Name, "namespace", req.Namespace)

	// Load the triggering CR to update its status.
	cfg := &monitoringv1alpha1.GatusAlertingConfig{}
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Resolve combined config (inline + configSecretRef).
	merged, requeueResult, err := r.resolveConfig(ctx, cfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeueResult != nil {
		return *requeueResult, nil
	}

	// Validate required fields against the merged config.
	missing := validateMergedConfig(cfg.Spec.Type, merged)
	if len(missing) > 0 {
		setCondition(&cfg.Status.Conditions, metav1.Condition{
			Type:    "Valid",
			Status:  metav1.ConditionFalse,
			Reason:  "MissingRequiredFields",
			Message: fmt.Sprintf("missing required fields: %v", missing),
		})
		if err := r.Status().Update(ctx, cfg); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update GatusAlertingConfig status: %w", err)
		}
		return ctrl.Result{}, nil
	}

	setCondition(&cfg.Status.Conditions, metav1.Condition{
		Type:    "Valid",
		Status:  metav1.ConditionTrue,
		Reason:  "ConfigValid",
		Message: "All required fields are present",
	})
	if err := r.Status().Update(ctx, cfg); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update GatusAlertingConfig status: %w", err)
	}

	// Aggregate all valid GatusAlertingConfig CRs.
	cfgList := &monitoringv1alpha1.GatusAlertingConfigList{}
	if err := r.List(ctx, cfgList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusAlertingConfigs: %w", err)
	}

	// Sort for deterministic ordering; first alphabetically by namespace/name wins per type.
	sort.Slice(cfgList.Items, func(i, j int) bool {
		ki := cfgList.Items[i].Namespace + "/" + cfgList.Items[i].Name
		kj := cfgList.Items[j].Namespace + "/" + cfgList.Items[j].Name
		return ki < kj
	})

	providers := make(map[string]interface{})
	for i := range cfgList.Items {
		item := &cfgList.Items[i]
		t := item.Spec.Type
		if _, exists := providers[t]; exists {
			logger.Info("Multiple GatusAlertingConfig CRs for the same type — skipping duplicate",
				"type", t, "name", item.Name, "namespace", item.Namespace)
			continue
		}
		itemMerged, _, resolveErr := r.resolveConfig(ctx, item)
		if resolveErr != nil {
			logger.Error(resolveErr, "Failed to resolve config for GatusAlertingConfig",
				"name", item.Name, "namespace", item.Namespace)
			continue
		}
		if itemMerged == nil || len(validateMergedConfig(t, itemMerged)) > 0 {
			continue // skip invalid or unresolvable configs
		}
		providers[t] = itemMerged
	}

	alerting := map[string]interface{}{"alerting": providers}
	data, err := yaml.Marshal(alerting)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal alerting config: %w", err)
	}

	return upsertSecretKey(ctx, r.Client, r.TargetNamespace, r.SecretName, alertingKey, string(data))
}

// resolveConfig loads the inline spec.config and merges in any values from the
// referenced Secret (configSecretRef). Returns the merged map, an optional requeue
// result (when the Secret is not yet available), and any unexpected error.
func (r *GatusAlertingConfigReconciler) resolveConfig(ctx context.Context, cfg *monitoringv1alpha1.GatusAlertingConfig) (map[string]interface{}, *ctrl.Result, error) {
	merged, err := apiExtMapToInterface(cfg.Spec.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal spec.config: %w", err)
	}

	if cfg.Spec.ConfigSecretRef == nil {
		return merged, nil, nil
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: cfg.Spec.ConfigSecretRef.Name, Namespace: r.ControllerNamespace}, secret); err != nil {
		if client.IgnoreNotFound(err) == nil {
			requeue := ctrl.Result{RequeueAfter: 10 * time.Second}
			return nil, &requeue, nil
		}
		return nil, nil, fmt.Errorf("failed to get configSecretRef %q: %w", cfg.Spec.ConfigSecretRef.Name, err)
	}

	// Merge secret data on top of spec.config (secret values win for the same key).
	for k, v := range secret.Data {
		merged[k] = string(v)
	}
	return merged, nil, nil
}

// apiExtMapToInterface converts a map[string]apiextv1.JSON to map[string]interface{}.
// Each JSON value is unmarshalled individually so it can be re-serialized to YAML.
func apiExtMapToInterface(m map[string]apiextv1.JSON) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		var val interface{}
		if err := json.Unmarshal(v.Raw, &val); err != nil {
			return nil, fmt.Errorf("failed to unmarshal key %q: %w", k, err)
		}
		out[k] = val
	}
	return out, nil
}

// validateMergedConfig returns the missing required fields for the given provider type
// against an already-merged config map (spec.config + configSecretRef data combined).
// An empty slice means the config is valid.
func validateMergedConfig(providerType string, merged map[string]interface{}) []string {
	required, ok := providerRequiredFields[providerType]
	if !ok {
		return nil
	}
	var missing []string
	for _, field := range required {
		if _, found := merged[field]; !found {
			missing = append(missing, field)
		}
	}
	return missing
}

func (r *GatusAlertingConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusAlertingConfig{}).
		Complete(r)
}
