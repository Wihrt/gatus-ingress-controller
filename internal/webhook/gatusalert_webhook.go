package webhook

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

// GatusAlertValidator validates GatusAlert resources.
type GatusAlertValidator struct {
	Client client.Client
}

func (v *GatusAlertValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	alert, ok := obj.(*monitoringv1alpha1.GatusAlert)
	if !ok {
		return nil, fmt.Errorf("expected a GatusAlert")
	}
	return nil, v.validateAlertProviderOverride(ctx, alert)
}

func (v *GatusAlertValidator) ValidateUpdate(ctx context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	alert, ok := newObj.(*monitoringv1alpha1.GatusAlert)
	if !ok {
		return nil, fmt.Errorf("expected a GatusAlert")
	}
	return nil, v.validateAlertProviderOverride(ctx, alert)
}

func (v *GatusAlertValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateAlertProviderOverride validates that all keys in spec.providerOverride are
// valid for the provider type resolved from the referenced GatusAlertingConfig.
func (v *GatusAlertValidator) validateAlertProviderOverride(ctx context.Context, alert *monitoringv1alpha1.GatusAlert) error {
	refPath := field.NewPath("spec").Child("alertingConfigRef")

	if alert.Spec.AlertingConfigRef == "" {
		return field.Required(refPath, "alertingConfigRef must reference a GatusAlertingConfig")
	}

	if len(alert.Spec.ProviderOverride) == 0 {
		// No overrides to validate; still check the ref exists if client is available.
		if v.Client != nil {
			cfg := &monitoringv1alpha1.GatusAlertingConfig{}
			if err := v.Client.Get(ctx, types.NamespacedName{Name: alert.Spec.AlertingConfigRef, Namespace: alert.Namespace}, cfg); err != nil {
				return field.Invalid(refPath, alert.Spec.AlertingConfigRef,
					fmt.Sprintf("referenced GatusAlertingConfig %q not found in namespace %q", alert.Spec.AlertingConfigRef, alert.Namespace))
			}
		}
		return nil
	}

	if v.Client == nil {
		return nil
	}

	cfg := &monitoringv1alpha1.GatusAlertingConfig{}
	if err := v.Client.Get(ctx, types.NamespacedName{Name: alert.Spec.AlertingConfigRef, Namespace: alert.Namespace}, cfg); err != nil {
		return field.Invalid(refPath, alert.Spec.AlertingConfigRef,
			fmt.Sprintf("referenced GatusAlertingConfig %q not found in namespace %q", alert.Spec.AlertingConfigRef, alert.Namespace))
	}

	providerType := cfg.Spec.Type
	allowed, known := providerAllowedFields[providerType]
	if !known {
		return field.Invalid(refPath, alert.Spec.AlertingConfigRef,
			fmt.Sprintf("referenced GatusAlertingConfig has unknown provider type %q", providerType))
	}

	var errs field.ErrorList
	overridePath := field.NewPath("spec").Child("providerOverride")
	for k := range alert.Spec.ProviderOverride {
		if _, ok := allowed[k]; !ok {
			errs = append(errs, field.Invalid(
				overridePath.Key(k),
				k,
				fmt.Sprintf("key %q is not allowed for provider %q; allowed keys are %v", k, providerType, toSortedKeys(allowed)),
			))
		}
	}
	if len(errs) > 0 {
		return errs.ToAggregate()
	}
	return nil
}
