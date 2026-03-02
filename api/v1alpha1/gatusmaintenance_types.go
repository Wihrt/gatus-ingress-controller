package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GatusMaintenanceSpec defines a global maintenance window during which Gatus suppresses all alerts.
// Only one GatusMaintenance resource is used at a time (the first alphabetically by name).
type GatusMaintenanceSpec struct {
	// Enabled indicates whether the maintenance window is active.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Start is the time at which the maintenance window begins, in "HH:MM" format (24-hour, UTC unless Timezone is set).
	// +kubebuilder:validation:Required
	Start string `json:"start"`

	// Duration is how long the maintenance window lasts (e.g. "1h", "30m").
	// +kubebuilder:validation:Required
	Duration string `json:"duration"`

	// Timezone is the timezone for the maintenance window (e.g. "Europe/Amsterdam", "UTC").
	// See https://en.wikipedia.org/wiki/List_of_tz_database_time_zones for valid values.
	// +optional
	Timezone string `json:"timezone,omitempty"`

	// Every is the list of days on which this window applies (e.g. ["Monday", "Thursday"]).
	// Leave empty to apply every day.
	// +optional
	Every []string `json:"every,omitempty"`
}

type GatusMaintenanceStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=gm
type GatusMaintenance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatusMaintenanceSpec   `json:"spec,omitempty"`
	Status GatusMaintenanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GatusMaintenanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatusMaintenance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatusMaintenance{}, &GatusMaintenanceList{})
}
