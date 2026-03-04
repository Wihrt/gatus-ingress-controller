package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GatusHeartbeatConfig configures automatic failure detection when the external endpoint stops sending updates.
type GatusHeartbeatConfig struct {
	// Interval is the expected duration between updates from the external endpoint (e.g. "30m", "1h").
	// If no update is received within this interval, alerts will be triggered.
	// Must be at least 10s. Set to "0" or leave empty to disable.
	// +optional
	Interval string `json:"interval,omitempty"`
}

// GatusExternalEndpointSpec defines the desired state of a GatusExternalEndpoint.
// Unlike regular endpoints, external endpoints are not monitored by Gatus directly.
// Their status is pushed programmatically via the Gatus API.
type GatusExternalEndpointSpec struct {
	// Name is the display name of the external endpoint in Gatus.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Enabled indicates whether this external endpoint is active.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Group is the group name used to organize endpoints on the Gatus dashboard.
	// +optional
	Group string `json:"group,omitempty"`

	// Token is the bearer token required to push status updates to this external endpoint.
	// +kubebuilder:validation:Required
	Token string `json:"token"`

	// Alerts is the list of alert configurations for this external endpoint.
	// +optional
	Alerts []GatusAlertSpec `json:"alerts,omitempty"`

	// Heartbeat configures automatic failure detection when updates stop being received.
	// +optional
	Heartbeat *GatusHeartbeatConfig `json:"heartbeat,omitempty"`
}

// GatusExternalEndpointStatus defines the observed state of GatusExternalEndpoint.
type GatusExternalEndpointStatus struct {
	// Conditions represent the latest available observations of a GatusExternalEndpoint's state.
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=gee
type GatusExternalEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatusExternalEndpointSpec   `json:"spec,omitempty"`
	Status GatusExternalEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GatusExternalEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatusExternalEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatusExternalEndpoint{}, &GatusExternalEndpointList{})
}
