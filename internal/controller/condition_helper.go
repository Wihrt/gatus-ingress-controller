package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// setCondition inserts or updates a condition in the given slice.
// It sets LastTransitionTime only when the Status changes.
func setCondition(conditions *[]metav1.Condition, newCond metav1.Condition) {
	now := metav1.Now()
	for i, c := range *conditions {
		if c.Type == newCond.Type {
			if c.Status != newCond.Status {
				newCond.LastTransitionTime = now
			} else {
				newCond.LastTransitionTime = c.LastTransitionTime
			}
			(*conditions)[i] = newCond
			return
		}
	}
	newCond.LastTransitionTime = now
	*conditions = append(*conditions, newCond)
}
