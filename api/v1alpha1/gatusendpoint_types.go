package v1alpha1

import (
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GatusAlertSpec defines an inline alert configuration for a GatusEndpoint or GatusExternalEndpoint.
// It mirrors the Gatus alert configuration format directly.
type GatusAlertSpec struct {
	// Type is the alert provider type (e.g. "slack", "discord", "pagerduty").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=awsses;clickup;custom;datadog;discord;email;gitea;github;gitlab;googlechat;gotify;homeassistant;ifttt;ilert;incident-io;line;matrix;mattermost;messagebird;n8n;newrelic;ntfy;opsgenie;pagerduty;plivo;pushover;rocketchat;sendgrid;signal;signl4;slack;splunk;squadcast;teams;teams-workflows;telegram;twilio;vonage;webex;zapier;zulip
	Type string `json:"type"`

	// Enabled indicates whether this alert is active.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// FailureThreshold is the number of consecutive failures before triggering the alert.
	// +kubebuilder:default=3
	// +optional
	FailureThreshold int `json:"failureThreshold,omitempty"`

	// SuccessThreshold is the number of consecutive successes before resolving an ongoing incident.
	// +kubebuilder:default=2
	// +optional
	SuccessThreshold int `json:"successThreshold,omitempty"`

	// SendOnResolved indicates whether to send a notification once a triggered alert is resolved.
	// +optional
	SendOnResolved *bool `json:"sendOnResolved,omitempty"`

	// Description is the description included in the alert notification.
	// +optional
	Description string `json:"description,omitempty"`

	// MinimumReminderInterval is the minimum duration between alert reminders (e.g. "30m", "1h").
	// Set to "0" or leave empty to disable reminders.
	// +optional
	MinimumReminderInterval string `json:"minimumReminderInterval,omitempty"`

	// ProviderOverride allows overriding specific provider configuration fields for this alert.
	// Maps directly to provider-override in the Gatus alert configuration.
	// +optional
	ProviderOverride map[string]apiextv1.JSON `json:"providerOverride,omitempty"`
}

// GatusDNSConfig holds DNS monitoring configuration.
type GatusDNSConfig struct {
	// QueryName is the domain name to query (e.g. "example.com").
	// +kubebuilder:validation:Required
	QueryName string `json:"queryName"`

	// QueryType is the DNS record type (e.g. "A", "MX", "AAAA").
	// +kubebuilder:default=A
	QueryType string `json:"queryType"`
}

// GatusSSHConfig holds SSH monitoring configuration.
type GatusSSHConfig struct {
	// Username is the SSH username.
	// +kubebuilder:validation:Required
	Username string `json:"username"`

	// Password is the SSH password.
	// +kubebuilder:validation:Required
	Password string `json:"password"`
}

// GatusClientOAuth2Config holds OAuth2 client credentials configuration.
type GatusClientOAuth2Config struct {
	// TokenURL is the token endpoint URL.
	// +kubebuilder:validation:Required
	TokenURL string `json:"tokenUrl"`

	// ClientID is the OAuth2 client ID.
	// +kubebuilder:validation:Required
	ClientID string `json:"clientId"`

	// ClientSecret is the OAuth2 client secret.
	// +kubebuilder:validation:Required
	ClientSecret string `json:"clientSecret"`

	// Scopes is the list of OAuth2 scopes.
	// +kubebuilder:validation:Required
	Scopes []string `json:"scopes"`
}

// GatusClientTLSConfig holds TLS client certificate configuration for mTLS.
type GatusClientTLSConfig struct {
	// CertificateFile is the path to the client certificate (PEM format).
	// +optional
	CertificateFile string `json:"certificateFile,omitempty"`

	// PrivateKeyFile is the path to the client private key (PEM format).
	// +optional
	PrivateKeyFile string `json:"privateKeyFile,omitempty"`

	// Renegotiation sets the TLS renegotiation type ("never", "freely", "once").
	// +kubebuilder:validation:Enum=never;freely;once
	// +optional
	Renegotiation string `json:"renegotiation,omitempty"`
}

// GatusClientConfig holds the HTTP client configuration for an endpoint.
type GatusClientConfig struct {
	// Insecure skips TLS certificate verification.
	// +optional
	Insecure bool `json:"insecure,omitempty"`

	// IgnoreRedirect disables following HTTP redirects.
	// +optional
	IgnoreRedirect bool `json:"ignoreRedirect,omitempty"`

	// Timeout is the request timeout (e.g. "10s", "30s").
	// +optional
	Timeout string `json:"timeout,omitempty"`

	// DNSResolver overrides the DNS resolver (format: "proto://host:port", e.g. "tcp://8.8.8.8:53").
	// +optional
	DNSResolver string `json:"dnsResolver,omitempty"`

	// ProxyURL is the URL of the HTTP proxy to use.
	// +optional
	ProxyURL string `json:"proxyUrl,omitempty"`

	// OAuth2 contains OAuth2 client credentials configuration.
	// +optional
	OAuth2 *GatusClientOAuth2Config `json:"oauth2,omitempty"`

	// TLS contains TLS client certificate configuration for mTLS.
	// +optional
	TLS *GatusClientTLSConfig `json:"tls,omitempty"`

	// Network sets the network type for ICMP endpoints ("ip", "ip4", "ip6").
	// +optional
	Network string `json:"network,omitempty"`
}

// GatusUIConfig holds UI display configuration for an endpoint.
type GatusUIConfig struct {
	// HideConditions hides conditions from the Gatus UI results.
	// +optional
	HideConditions bool `json:"hideConditions,omitempty"`

	// HideHostname hides the hostname in the Gatus UI results.
	// +optional
	HideHostname bool `json:"hideHostname,omitempty"`

	// HidePort hides the port from the Gatus UI results.
	// +optional
	HidePort bool `json:"hidePort,omitempty"`

	// HideURL hides the URL in the Gatus UI results. Useful when the URL contains a token.
	// +optional
	HideURL bool `json:"hideUrl,omitempty"`

	// HideErrors hides errors from the Gatus UI results.
	// +optional
	HideErrors bool `json:"hideErrors,omitempty"`

	// DontResolveFailedConditions disables condition resolution in the UI for failed checks.
	// +optional
	DontResolveFailedConditions bool `json:"dontResolveFailedConditions,omitempty"`

	// ResolveSuccessfulConditions enables condition resolution in the UI even for successful checks.
	// +optional
	ResolveSuccessfulConditions bool `json:"resolveSuccessfulConditions,omitempty"`
}

// GatusMaintenanceWindow defines a recurring maintenance window during which alerts are suppressed.
type GatusMaintenanceWindow struct {
	// Day is the day of week (e.g. "monday"). Use Every for multiple days.
	// +optional
	Day string `json:"day,omitempty"`

	// Every is a list of days of the week for this window (e.g. ["monday", "thursday"]).
	// +optional
	Every []string `json:"every,omitempty"`

	// Start is the start time in "HH:MM" format (24-hour).
	// +kubebuilder:validation:Required
	Start string `json:"start"`

	// Duration is how long the maintenance window lasts (e.g. "1h", "30m").
	// +kubebuilder:validation:Required
	Duration string `json:"duration"`

	// Timezone is the timezone for the window (e.g. "UTC", "US/Eastern").
	// +optional
	Timezone string `json:"timezone,omitempty"`
}

// GatusEndpointSpec defines the desired monitoring configuration for a GatusEndpoint.
type GatusEndpointSpec struct {
	// Name is the display name of the endpoint in Gatus.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Enabled indicates whether Gatus should monitor this endpoint.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Group is the group name used to organize endpoints on the Gatus dashboard.
	// +optional
	Group string `json:"group,omitempty"`

	// URL is the endpoint URL to monitor.
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// Method is the HTTP request method (e.g. "GET", "POST").
	// +kubebuilder:default=GET
	// +optional
	Method string `json:"method,omitempty"`

	// Interval is the duration between health checks (e.g. "60s", "5m").
	// +optional
	Interval string `json:"interval,omitempty"`

	// Body is the HTTP request body.
	// +optional
	Body string `json:"body,omitempty"`

	// Headers contains HTTP request headers as key-value pairs.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`

	// GraphQL wraps the body in a GraphQL query param ({"query":"$body"}).
	// +optional
	GraphQL bool `json:"graphql,omitempty"`

	// Conditions are the health conditions to evaluate for this endpoint.
	// See https://github.com/TwiN/gatus#conditions for syntax.
	// +optional
	Conditions []string `json:"conditions,omitempty"`

	// Alerts is the list of alert configurations for this endpoint.
	// +optional
	Alerts []GatusAlertSpec `json:"alerts,omitempty"`

	// DNS contains DNS-specific monitoring configuration.
	// +optional
	DNS *GatusDNSConfig `json:"dns,omitempty"`

	// SSH contains SSH-specific monitoring configuration.
	// +optional
	SSH *GatusSSHConfig `json:"ssh,omitempty"`

	// Client contains the HTTP client configuration for this endpoint.
	// +optional
	Client *GatusClientConfig `json:"client,omitempty"`

	// UI contains UI display configuration for this endpoint.
	// +optional
	UI *GatusUIConfig `json:"ui,omitempty"`

	// MaintenanceWindows defines recurring windows during which alerts are suppressed.
	// +optional
	MaintenanceWindows []GatusMaintenanceWindow `json:"maintenanceWindows,omitempty"`

	// ExtraLabels are additional labels added to metrics for this endpoint.
	// +optional
	ExtraLabels map[string]string `json:"extraLabels,omitempty"`
}

type GatusEndpointStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ge
type GatusEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatusEndpointSpec   `json:"spec,omitempty"`
	Status GatusEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GatusEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatusEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatusEndpoint{}, &GatusEndpointList{})
}
