package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	monitoringv1alpha1 "github.com/Wihrt/gatus-controller/api/v1alpha1"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	if err := monitoringv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add monitoring scheme: %v", err)
	}
	return s
}
