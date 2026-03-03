package v1alpha1

import (
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigSecretRef references a Kubernetes Secret whose data keys are merged
// into the provider configuration. Sensitive fields (API keys, webhook URLs, etc.)
// should be stored in the Secret rather than inlined in spec.config.
// The Secret must reside in the same namespace as the controller.
// Secret values are treated as UTF-8 strings and merged on top of spec.config.
type ConfigSecretRef struct {
	// Name of the Secret in the controller's namespace.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// GatusAlertingConfigSpec configures a single Gatus alert provider.
// Each instance represents exactly one provider type.
type GatusAlertingConfigSpec struct {
	// Type is the alert provider type.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=awsses;clickup;custom;datadog;discord;email;gitea;github;gitlab;googlechat;gotify;homeassistant;ifttt;ilert;incident-io;line;matrix;mattermost;messagebird;n8n;newrelic;ntfy;opsgenie;pagerduty;plivo;pushover;rocketchat;sendgrid;signal;signl4;slack;splunk;squadcast;teams;teams-workflows;telegram;twilio;vonage;webex;zapier;zulip
	Type string `json:"type"`

	// Config holds non-sensitive provider-specific configuration key-value pairs.
	// These map directly to the Gatus alerting.<type> configuration block.
	// Values can be strings, numbers, booleans, or arrays.
	// +optional
	Config map[string]apiextv1.JSON `json:"config,omitempty"`

	// ConfigSecretRef references a Secret whose data keys are merged into the
	// provider configuration on top of spec.config. Use this for sensitive fields
	// such as API keys, webhook URLs, and passwords. The Secret is typically
	// managed by an operator such as External Secrets Operator.
	// +optional
	ConfigSecretRef *ConfigSecretRef `json:"configSecretRef,omitempty"`
}

// GatusAlertingConfigStatus holds the observed state of the GatusAlertingConfig.
type GatusAlertingConfigStatus struct {
	// Conditions holds validation status for this alerting config.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=gac
type GatusAlertingConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatusAlertingConfigSpec   `json:"spec,omitempty"`
	Status GatusAlertingConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GatusAlertingConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatusAlertingConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatusAlertingConfig{}, &GatusAlertingConfigList{})
}
