package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

const (
	endpointsKey = "endpoints.yaml"
)

// GatusEndpointReconciler reconciles GatusEndpoint resources and aggregates them
// into a Secret containing the Gatus endpoints configuration.
type GatusEndpointReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	TargetNamespace string
	SecretName      string
}

// --- Internal YAML representation types (matching Gatus config format) ---

type gatusConfigFile struct {
	Endpoints []gatusEndpointYAML `yaml:"endpoints"`
}

type gatusEndpointYAML struct {
	Name               string                    `yaml:"name"`
	Enabled            *bool                     `yaml:"enabled,omitempty"`
	Group              string                    `yaml:"group,omitempty"`
	URL                string                    `yaml:"url"`
	Method             string                    `yaml:"method,omitempty"`
	Interval           string                    `yaml:"interval,omitempty"`
	Body               string                    `yaml:"body,omitempty"`
	Headers            map[string]string         `yaml:"headers,omitempty"`
	GraphQL            bool                      `yaml:"graphql,omitempty"`
	Conditions         []string                  `yaml:"conditions,omitempty"`
	Alerts             []gatusAlertYAML          `yaml:"alerts,omitempty"`
	DNS                *gatusDNSYAML             `yaml:"dns,omitempty"`
	SSH                *gatusSSHYAML             `yaml:"ssh,omitempty"`
	Client             *gatusClientYAML          `yaml:"client,omitempty"`
	UI                 *gatusUIYAML              `yaml:"ui,omitempty"`
	MaintenanceWindows []gatusMaintenanceWinYAML `yaml:"maintenance-windows,omitempty"`
	ExtraLabels        map[string]string         `yaml:"extra-labels,omitempty"`
}

type gatusAlertYAML struct {
	Type                    string                 `yaml:"type"`
	Enabled                 *bool                  `yaml:"enabled,omitempty"`
	Description             string                 `yaml:"description,omitempty"`
	FailureThreshold        int                    `yaml:"failure-threshold,omitempty"`
	SuccessThreshold        int                    `yaml:"success-threshold,omitempty"`
	SendOnResolved          *bool                  `yaml:"send-on-resolved,omitempty"`
	MinimumReminderInterval string                 `yaml:"minimum-reminder-interval,omitempty"`
	ProviderOverride        map[string]interface{} `yaml:"provider-override,omitempty"`
}

type gatusDNSYAML struct {
	QueryName string `yaml:"query-name"`
	QueryType string `yaml:"query-type"`
}

type gatusSSHYAML struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type gatusClientYAML struct {
	Insecure       bool                   `yaml:"insecure,omitempty"`
	IgnoreRedirect bool                   `yaml:"ignore-redirect,omitempty"`
	Timeout        string                 `yaml:"timeout,omitempty"`
	DNSResolver    string                 `yaml:"dns-resolver,omitempty"`
	ProxyURL       string                 `yaml:"proxy-url,omitempty"`
	OAuth2         *gatusClientOAuth2YAML `yaml:"oauth2,omitempty"`
	TLS            *gatusClientTLSYAML    `yaml:"tls,omitempty"`
	Network        string                 `yaml:"network,omitempty"`
}

type gatusClientOAuth2YAML struct {
	TokenURL     string   `yaml:"token-url"`
	ClientID     string   `yaml:"client-id"`
	ClientSecret string   `yaml:"client-secret"`
	Scopes       []string `yaml:"scopes"`
}

type gatusClientTLSYAML struct {
	CertificateFile string `yaml:"certificate-file,omitempty"`
	PrivateKeyFile  string `yaml:"private-key-file,omitempty"`
	Renegotiation   string `yaml:"renegotiation,omitempty"`
}

type gatusUIYAML struct {
	HideConditions              bool `yaml:"hide-conditions,omitempty"`
	HideHostname                bool `yaml:"hide-hostname,omitempty"`
	HidePort                    bool `yaml:"hide-port,omitempty"`
	HideURL                     bool `yaml:"hide-url,omitempty"`
	HideErrors                  bool `yaml:"hide-errors,omitempty"`
	DontResolveFailedConditions bool `yaml:"dont-resolve-failed-conditions,omitempty"`
	ResolveSuccessfulConditions bool `yaml:"resolve-successful-conditions,omitempty"`
}

type gatusMaintenanceWinYAML struct {
	Day      string   `yaml:"day,omitempty"`
	Every    []string `yaml:"every,omitempty"`
	Start    string   `yaml:"start"`
	Duration string   `yaml:"duration"`
	Timezone string   `yaml:"timezone,omitempty"`
}

// Reconcile aggregates all GatusEndpoints and writes endpoints.yaml to the target Secret.
func (r *GatusEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusEndpoint", "name", req.Name, "namespace", req.Namespace)

	// List all GatusEndpoints across all namespaces.
	endpointList := &monitoringv1alpha1.GatusEndpointList{}
	if err := r.List(ctx, endpointList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list GatusEndpoints: %w", err)
	}

	// Sort for deterministic output; first alphabetically (namespace/name) wins on spec.name conflict.
	sort.Slice(endpointList.Items, func(i, j int) bool {
		ki := endpointList.Items[i].Namespace + "/" + endpointList.Items[i].Name
		kj := endpointList.Items[j].Namespace + "/" + endpointList.Items[j].Name
		return ki < kj
	})

	// Deduplicate by spec.name: first alphabetically wins; log a warning for conflicts.
	seenNames := make(map[string]string, len(endpointList.Items)) // spec.name -> namespace/name of keeper
	var endpoints []gatusEndpointYAML
	for _, ep := range endpointList.Items {
		if keeper, conflict := seenNames[ep.Spec.Name]; conflict {
			logger.Info("Duplicate spec.name detected — keeping first alphabetically, skipping second",
				"spec.name", ep.Spec.Name,
				"kept", keeper,
				"skipped", ep.Namespace+"/"+ep.Name)
			continue
		}
		seenNames[ep.Spec.Name] = ep.Namespace + "/" + ep.Name

		alertYAMLs := convertAlerts(ep.Spec.Alerts)

		conditions := ep.Spec.Conditions
		if len(conditions) == 0 {
			conditions = []string{"[STATUS] == 200"}
		}

		epYAML := gatusEndpointYAML{
			Name:        ep.Spec.Name,
			Enabled:     boolPtr(ep.Spec.Enabled),
			Group:       ep.Spec.Group,
			URL:         ep.Spec.URL,
			Method:      ep.Spec.Method,
			Interval:    ep.Spec.Interval,
			Body:        ep.Spec.Body,
			Headers:     ep.Spec.Headers,
			GraphQL:     ep.Spec.GraphQL,
			Conditions:  conditions,
			Alerts:      alertYAMLs,
			ExtraLabels: ep.Spec.ExtraLabels,
		}

		if ep.Spec.DNS != nil {
			epYAML.DNS = &gatusDNSYAML{
				QueryName: ep.Spec.DNS.QueryName,
				QueryType: ep.Spec.DNS.QueryType,
			}
		}

		if ep.Spec.SSH != nil {
			epYAML.SSH = &gatusSSHYAML{
				Username: ep.Spec.SSH.Username,
				Password: ep.Spec.SSH.Password,
			}
		}

		if ep.Spec.Client != nil {
			epYAML.Client = buildClientYAML(ep.Spec.Client)
		}

		if ep.Spec.UI != nil {
			epYAML.UI = &gatusUIYAML{
				HideConditions:              ep.Spec.UI.HideConditions,
				HideHostname:                ep.Spec.UI.HideHostname,
				HidePort:                    ep.Spec.UI.HidePort,
				HideURL:                     ep.Spec.UI.HideURL,
				HideErrors:                  ep.Spec.UI.HideErrors,
				DontResolveFailedConditions: ep.Spec.UI.DontResolveFailedConditions,
				ResolveSuccessfulConditions: ep.Spec.UI.ResolveSuccessfulConditions,
			}
		}

		for _, mw := range ep.Spec.MaintenanceWindows {
			epYAML.MaintenanceWindows = append(epYAML.MaintenanceWindows, gatusMaintenanceWinYAML{
				Day:      mw.Day,
				Every:    mw.Every,
				Start:    mw.Start,
				Duration: mw.Duration,
				Timezone: mw.Timezone,
			})
		}

		endpoints = append(endpoints, epYAML)
	}

	cfg := gatusConfigFile{Endpoints: endpoints}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal Gatus endpoints config: %w", err)
	}

	return upsertSecretKey(ctx, r.Client, r.TargetNamespace, r.SecretName, endpointsKey, string(data))
}

// convertAlerts converts inline GatusAlertSpec entries to YAML alert configs.
func convertAlerts(alerts []monitoringv1alpha1.GatusAlertSpec) []gatusAlertYAML {
	var out []gatusAlertYAML
	for _, a := range alerts {
		y := gatusAlertYAML{
			Type:                    a.Type,
			Enabled:                 a.Enabled,
			Description:             a.Description,
			FailureThreshold:        a.FailureThreshold,
			SuccessThreshold:        a.SuccessThreshold,
			SendOnResolved:          a.SendOnResolved,
			MinimumReminderInterval: a.MinimumReminderInterval,
		}

		if len(a.ProviderOverride) > 0 {
			overrideMap := apiExtMapToInterface(a.ProviderOverride)
			y.ProviderOverride = overrideMap
		}

		out = append(out, y)
	}
	return out
}

// apiExtMapToInterface converts a map[string]apiextv1.JSON to map[string]interface{}.
func apiExtMapToInterface(m map[string]apiextv1.JSON) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		var val interface{}
		if err := json.Unmarshal(v.Raw, &val); err != nil {
			continue
		}
		out[k] = val
	}
	return out
}

func buildClientYAML(c *monitoringv1alpha1.GatusClientConfig) *gatusClientYAML {
	y := &gatusClientYAML{
		Insecure:       c.Insecure,
		IgnoreRedirect: c.IgnoreRedirect,
		Timeout:        c.Timeout,
		DNSResolver:    c.DNSResolver,
		ProxyURL:       c.ProxyURL,
		Network:        c.Network,
	}
	if c.OAuth2 != nil {
		y.OAuth2 = &gatusClientOAuth2YAML{
			TokenURL:     c.OAuth2.TokenURL,
			ClientID:     c.OAuth2.ClientID,
			ClientSecret: c.OAuth2.ClientSecret,
			Scopes:       c.OAuth2.Scopes,
		}
	}
	if c.TLS != nil {
		y.TLS = &gatusClientTLSYAML{
			CertificateFile: c.TLS.CertificateFile,
			PrivateKeyFile:  c.TLS.PrivateKeyFile,
			Renegotiation:   c.TLS.Renegotiation,
		}
	}
	return y
}

func (r *GatusEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusEndpoint{}).
		Complete(r)
}
