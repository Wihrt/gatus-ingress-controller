package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GatusAnnouncementSpec defines an announcement to display on the Gatus status page.
type GatusAnnouncementSpec struct {
	// Timestamp is the UTC time when the announcement was made (RFC3339 format, e.g. "2025-11-07T14:00:00Z").
	// +kubebuilder:validation:Required
	Timestamp string `json:"timestamp"`

	// Type is the severity/category of the announcement.
	// +kubebuilder:validation:Enum=outage;warning;information;operational;none
	// +kubebuilder:default=none
	Type string `json:"type"`

	// Message is the announcement text. Markdown is supported.
	// +kubebuilder:validation:Required
	Message string `json:"message"`

	// Archived moves the announcement to the "Past Announcements" section at the bottom of the status page.
	// +optional
	Archived bool `json:"archived,omitempty"`
}

type GatusAnnouncementStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=gan
type GatusAnnouncement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatusAnnouncementSpec   `json:"spec,omitempty"`
	Status GatusAnnouncementStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GatusAnnouncementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatusAnnouncement `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatusAnnouncement{}, &GatusAnnouncementList{})
}
