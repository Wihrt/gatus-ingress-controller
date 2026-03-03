package v1alpha1

import (
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GatusAlertSpec struct {
	// AlertingConfigRef is the name of a GatusAlertingConfig in the same namespace.
	// The provider type is resolved from the referenced object at runtime.
	// +kubebuilder:validation:Required
	AlertingConfigRef string `json:"alertingConfigRef"`

	// Enabled indicates whether this alert is enabled globally.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// FailureThreshold is the default number of consecutive failures before triggering the alert.
	// Can be overridden at the endpoint level via GatusAlertRef.
	// +kubebuilder:default=3
	FailureThreshold int `json:"failureThreshold"`

	// SuccessThreshold is the default number of consecutive successes before resolving an ongoing incident.
	// Can be overridden at the endpoint level via GatusAlertRef.
	// +kubebuilder:default=2
	SuccessThreshold int `json:"successThreshold"`

	// SendOnResolved indicates whether to send a notification once a triggered alert is resolved.
	// Can be overridden at the endpoint level via GatusAlertRef.
	// +optional
	SendOnResolved bool `json:"sendOnResolved,omitempty"`

	// Description is the default description included in the alert notification.
	// Can be overridden at the endpoint level via GatusAlertRef.
	// +optional
	Description string `json:"description,omitempty"`

	// MinimumReminderInterval is the minimum duration between alert reminders (e.g. "30m", "1h").
	// Set to "0" or leave empty to disable reminders.
	// +optional
	MinimumReminderInterval string `json:"minimumReminderInterval,omitempty"`

	// ProviderOverride allows overriding specific provider configuration fields for this alert.
	// Maps directly to provider-override in the Gatus alert configuration.
	// Only keys valid for the provider type are accepted (enforced by the admission webhook).
	// +optional
	ProviderOverride map[string]apiextv1.JSON `json:"providerOverride,omitempty"`
}

type GatusAlertStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ga
type GatusAlert struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatusAlertSpec   `json:"spec,omitempty"`
	Status GatusAlertStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GatusAlertList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatusAlert `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatusAlert{}, &GatusAlertList{})
}
