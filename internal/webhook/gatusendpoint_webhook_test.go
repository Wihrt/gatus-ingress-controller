package webhook

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func makeEndpoint(conditions []string) *monitoringv1alpha1.GatusEndpoint {
	return &monitoringv1alpha1.GatusEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ep", Namespace: "default"},
		Spec: monitoringv1alpha1.GatusEndpointSpec{
			Name:       "Test EP",
			URL:        "https://example.com",
			Conditions: conditions,
		},
	}
}

func TestEndpointWebhook_AllowsValidStatusCondition(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"[STATUS] == 200"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err != nil {
		t.Errorf("expected valid condition to be accepted, got: %v", err)
	}
}

func TestEndpointWebhook_AllowsBodyJSONPath(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"[BODY].user.name == john"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err != nil {
		t.Errorf("expected valid body condition to be accepted, got: %v", err)
	}
}

func TestEndpointWebhook_AllowsLenFunction(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"len([BODY].data) < 5"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err != nil {
		t.Errorf("expected len([BODY]) condition to be accepted, got: %v", err)
	}
}

func TestEndpointWebhook_AllowsResponseTime(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"[RESPONSE_TIME] < 500"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err != nil {
		t.Errorf("expected [RESPONSE_TIME] condition to be accepted, got: %v", err)
	}
}

func TestEndpointWebhook_AllowsCertificateExpiration(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"[CERTIFICATE_EXPIRATION] > 48h"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err != nil {
		t.Errorf("expected [CERTIFICATE_EXPIRATION] condition to be accepted, got: %v", err)
	}
}

func TestEndpointWebhook_RejectsUnknownPlaceholder(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"[FOOBAR] == 200"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err == nil {
		t.Error("expected unknown placeholder to be rejected")
	}
}

func TestEndpointWebhook_RejectsMissingOperator(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"[STATUS] 200"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err == nil {
		t.Error("expected missing operator to be rejected")
	}
}

func TestEndpointWebhook_RejectsEmptyCondition(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{""})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err == nil {
		t.Error("expected empty condition to be rejected")
	}
}

func TestEndpointWebhook_AllowsNoConditions(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint(nil)
	_, err := v.ValidateCreate(context.Background(), ep)
	if err != nil {
		t.Errorf("expected endpoint with no conditions to be accepted, got: %v", err)
	}
}

func TestEndpointWebhook_ValidateUpdate(t *testing.T) {
	v := &GatusEndpointValidator{}
	old := makeEndpoint([]string{"[STATUS] == 200"})
	updated := makeEndpoint([]string{"[FOOBAR] == 500"})
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Error("expected invalid updated condition to be rejected")
	}
}

func TestEndpointWebhook_ValidateDelete(t *testing.T) {
	v := &GatusEndpointValidator{}
	ep := makeEndpoint([]string{"[STATUS] == 200"})
	_, err := v.ValidateDelete(context.Background(), ep)
	if err != nil {
		t.Errorf("ValidateDelete should always succeed, got: %v", err)
	}
}

func TestValidateCondition_LenWithNonBody(t *testing.T) {
	fld := field.NewPath("spec").Child("conditions").Index(0)
	err := validateCondition("len([STATUS]) < 5", fld)
	if err == nil {
		t.Error("expected len() with non-BODY placeholder to be rejected")
	}
}

func TestEndpointWebhook_RejectsMixedValidAndUnknownPlaceholder(t *testing.T) {
	v := &GatusEndpointValidator{}
	// [STATUS] is valid but [FOO] is not; the condition should be rejected.
	ep := makeEndpoint([]string{"[STATUS] == 200 && [FOO] != 0"})
	_, err := v.ValidateCreate(context.Background(), ep)
	if err == nil {
		t.Error("expected condition with unknown placeholder [FOO] to be rejected even when a valid placeholder is also present")
	}
}
