package webhook

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

// providerAllowedFields lists every valid key in spec.config for each provider type.
// Keys not in this set are rejected at admission time.
// All providers also accept "default-alert" and "overrides" — those are included per-entry.
var providerAllowedFields = map[string]map[string]struct{}{
	"awsses":  toSet("access-key-id", "secret-access-key", "region", "from", "to", "default-alert", "overrides"),
	"clickup": toSet("list-id", "token", "default-alert", "overrides"),
	// custom is intentionally open: users may add arbitrary headers / body fields.
	// We still allow the well-known keys plus the two common ones.
	"custom":          toSet("url", "method", "headers", "body", "insecure", "default-alert", "overrides"),
	"datadog":         toSet("api-key", "account-id", "default-alert", "overrides"),
	"discord":         toSet("webhook-url", "title", "message-content", "default-alert", "overrides"),
	"email":           toSet("from", "username", "password", "host", "port", "to", "client", "default-alert", "overrides"),
	"gitea":           toSet("repository-url", "token", "default-alert", "overrides"),
	"github":          toSet("repository-url", "token", "default-alert", "overrides"),
	"gitlab":          toSet("webhook-url", "authorization-key", "default-alert", "overrides"),
	"googlechat":      toSet("webhook-url", "default-alert", "overrides"),
	"gotify":          toSet("server-url", "token", "default-alert", "overrides"),
	"homeassistant":   toSet("url", "token", "default-alert", "overrides"),
	"ifttt":           toSet("webhook-key", "event-name", "default-alert", "overrides"),
	"ilert":           toSet("integration-key", "default-alert", "overrides"),
	"incident-io":     toSet("url", "auth-token", "default-alert", "overrides"),
	"line":            toSet("channel-access-token", "user-ids", "default-alert", "overrides"),
	"matrix":          toSet("access-token", "internal-room-id", "server-url", "default-alert", "overrides"),
	"mattermost":      toSet("webhook-url", "default-alert", "overrides"),
	"messagebird":     toSet("access-key", "originator", "recipients", "default-alert", "overrides"),
	"n8n":             toSet("webhook-url", "default-alert", "overrides"),
	"newrelic":        toSet("api-key", "account-id", "default-alert", "overrides"),
	"ntfy":            toSet("topic", "url", "token", "username", "password", "priority", "default-alert", "overrides"),
	"opsgenie":        toSet("api-key", "default-alert", "overrides"),
	"pagerduty":       toSet("integration-key", "default-alert", "overrides"),
	"plivo":           toSet("auth-id", "auth-token", "from", "to", "default-alert", "overrides"),
	"pushover":        toSet("application-token", "user-key", "default-alert", "overrides"),
	"rocketchat":      toSet("webhook-url", "default-alert", "overrides"),
	"sendgrid":        toSet("api-key", "from", "to", "default-alert", "overrides"),
	"signal":          toSet("cli-client-url", "default-alert", "overrides"),
	"signl4":          toSet("token", "default-alert", "overrides"),
	"slack":           toSet("webhook-url", "title", "default-alert", "overrides"),
	"splunk":          toSet("token", "url", "default-alert", "overrides"),
	"squadcast":       toSet("webhook-url", "default-alert", "overrides"),
	"teams":           toSet("webhook-url", "default-alert", "overrides"),
	"teams-workflows": toSet("webhook-url", "default-alert", "overrides"),
	"telegram":        toSet("token", "id", "api-server-url", "default-alert", "overrides"),
	"twilio":          toSet("sid", "token", "from", "to", "default-alert", "overrides"),
	"vonage":          toSet("api-key", "api-secret", "from", "to", "default-alert", "overrides"),
	"webex":           toSet("webhook-url", "default-alert", "overrides"),
	"zapier":          toSet("webhook-url", "default-alert", "overrides"),
	"zulip":           toSet("bot-email", "bot-api-key", "domain", "channel", "topic", "default-alert", "overrides"),
}

// providerRequiredFields mirrors the same map in the controller package.
// It is duplicated here so the webhook package has no import cycle with the controller package.
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

// toSet converts a list of strings to a set (map[string]struct{}).
func toSet(keys ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

func toSortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GatusAlertingConfigValidator implements webhook.CustomValidator for GatusAlertingConfig.
type GatusAlertingConfigValidator struct {
	Client client.Client
}

// ValidateCreate validates a new GatusAlertingConfig object.
func (v *GatusAlertingConfigValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cfg, ok := obj.(*monitoringv1alpha1.GatusAlertingConfig)
	if !ok {
		return nil, fmt.Errorf("expected a GatusAlertingConfig, got %T", obj)
	}
	errs := validate(cfg)
	if uniqueErr := v.validateUniqueness(ctx, cfg, ""); uniqueErr != nil {
		errs = append(errs, uniqueErr...)
	}
	return nil, errs.ToAggregate()
}

// ValidateUpdate validates an updated GatusAlertingConfig object.
func (v *GatusAlertingConfigValidator) ValidateUpdate(ctx context.Context, oldObj runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	cfg, ok := newObj.(*monitoringv1alpha1.GatusAlertingConfig)
	if !ok {
		return nil, fmt.Errorf("expected a GatusAlertingConfig, got %T", newObj)
	}
	old, ok := oldObj.(*monitoringv1alpha1.GatusAlertingConfig)
	if !ok {
		return nil, fmt.Errorf("expected a GatusAlertingConfig (old), got %T", oldObj)
	}
	errs := validate(cfg)
	// Allow updating the same object (same name+namespace); skip uniqueness if type unchanged.
	if cfg.Spec.Type != old.Spec.Type {
		if uniqueErr := v.validateUniqueness(ctx, cfg, cfg.Name+"/"+cfg.Namespace); uniqueErr != nil {
			errs = append(errs, uniqueErr...)
		}
	}
	return nil, errs.ToAggregate()
}

// ValidateDelete is a no-op: deletions are always allowed.
func (v *GatusAlertingConfigValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateUniqueness checks that no other GatusAlertingConfig with the same spec.type exists cluster-wide.
// excludeKey is "name/namespace" of the object being updated (skipped in the list); empty on create.
func (v *GatusAlertingConfigValidator) validateUniqueness(ctx context.Context, cfg *monitoringv1alpha1.GatusAlertingConfig, excludeKey string) field.ErrorList {
	if v.Client == nil {
		return nil
	}
	list := &monitoringv1alpha1.GatusAlertingConfigList{}
	if err := v.Client.List(ctx, list); err != nil {
		return field.ErrorList{field.InternalError(
			field.NewPath("spec").Child("type"),
			fmt.Errorf("failed to list GatusAlertingConfigs for uniqueness check: %w", err),
		)}
	}
	for _, existing := range list.Items {
		if existing.Spec.Type != cfg.Spec.Type {
			continue
		}
		key := existing.Name + "/" + existing.Namespace
		if key == excludeKey {
			continue
		}
		return field.ErrorList{field.Invalid(
			field.NewPath("spec").Child("type"),
			cfg.Spec.Type,
			fmt.Sprintf("a GatusAlertingConfig of type %q already exists (%s in namespace %s); only one per type is allowed cluster-wide",
				cfg.Spec.Type, existing.Name, existing.Namespace),
		)}
	}
	return nil
}

// validate performs all field-level validation on a GatusAlertingConfig and returns
// a list of field errors. It checks:
//  1. No unknown keys in spec.config (always checked).
//  2. All required fields for the provider type are present in spec.config
//     (only checked when configSecretRef is NOT set — required fields may come from the Secret).
func validate(cfg *monitoringv1alpha1.GatusAlertingConfig) field.ErrorList {
	var errs field.ErrorList
	configPath := field.NewPath("spec").Child("config")

	allowed, hasAllowed := providerAllowedFields[cfg.Spec.Type]

	// 1. Reject unknown keys.
	if hasAllowed {
		for k := range cfg.Spec.Config {
			if _, ok := allowed[k]; !ok {
				errs = append(errs, field.Invalid(
					configPath.Key(k),
					k,
					fmt.Sprintf("unknown field %q for provider type %q", k, cfg.Spec.Type),
				))
			}
		}
	}

	// 2. Reject missing required fields — only when no configSecretRef is set.
	if cfg.Spec.ConfigSecretRef == nil {
		for _, req := range providerRequiredFields[cfg.Spec.Type] {
			if _, present := cfg.Spec.Config[req]; !present {
				errs = append(errs, field.Required(
					configPath.Key(req),
					fmt.Sprintf("required field %q for provider type %q", req, cfg.Spec.Type),
				))
			}
		}
	}

	return errs
}
