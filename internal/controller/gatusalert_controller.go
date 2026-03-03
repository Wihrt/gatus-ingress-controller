package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

// GatusAlertReconciler validates GatusAlert CRs by checking if a corresponding
// GatusAlertingConfig of the same provider type exists. Sets the Configured status condition.
type GatusAlertReconciler struct {
	client.Client
}

func (r *GatusAlertReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GatusAlert", "name", req.Name, "namespace", req.Namespace)

	alert := &monitoringv1alpha1.GatusAlert{}
	if err := r.Get(ctx, req.NamespacedName, alert); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	configured := false
	condReason := "AlertingConfigNotFound"
	condMessage := fmt.Sprintf("GatusAlertingConfig %q not found in namespace %q", alert.Spec.AlertingConfigRef, req.Namespace)

	if alert.Spec.AlertingConfigRef != "" {
		cfg := &monitoringv1alpha1.GatusAlertingConfig{}
		err := r.Get(ctx, client.ObjectKey{Name: alert.Spec.AlertingConfigRef, Namespace: req.Namespace}, cfg)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to get GatusAlertingConfig", "name", alert.Spec.AlertingConfigRef, "namespace", req.Namespace)
			return ctrl.Result{}, fmt.Errorf("failed to get GatusAlertingConfig %q: %w", alert.Spec.AlertingConfigRef, err)
		}
		if err == nil {
			// Trust the GatusAlertingConfig's own status condition.
			for _, cond := range cfg.Status.Conditions {
				if cond.Type == "Valid" && cond.Status == metav1.ConditionTrue {
					configured = true
					break
				}
			}
			if !configured {
				condReason = "AlertingConfigInvalid"
				condMessage = fmt.Sprintf("GatusAlertingConfig %q exists but is not Valid", alert.Spec.AlertingConfigRef)
			}
		}
	} else {
		condReason = "MissingAlertingConfigRef"
		condMessage = "spec.alertingConfigRef must be set"
	}

	condStatus := metav1.ConditionTrue
	if configured {
		condReason = "AlertingConfigFound"
		condMessage = fmt.Sprintf("GatusAlertingConfig %q is valid", alert.Spec.AlertingConfigRef)
	} else {
		condStatus = metav1.ConditionFalse
	}

	setCondition(&alert.Status.Conditions, metav1.Condition{
		Type:    "Configured",
		Status:  condStatus,
		Reason:  condReason,
		Message: condMessage,
	})
	if err := r.Status().Update(ctx, alert); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update GatusAlert status: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *GatusAlertReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.GatusAlert{}).
		Watches(
			&monitoringv1alpha1.GatusAlertingConfig{},
			handler.EnqueueRequestsFromMapFunc(r.alertsForConfig),
		).
		Complete(r)
}

// alertsForConfig returns reconcile requests for all GatusAlerts in the same
// namespace that reference the changed GatusAlertingConfig.
func (r *GatusAlertReconciler) alertsForConfig(ctx context.Context, obj client.Object) []reconcile.Request {
	alertList := &monitoringv1alpha1.GatusAlertList{}
	if err := r.List(ctx, alertList, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for _, alert := range alertList.Items {
		if alert.Spec.AlertingConfigRef == obj.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: alert.Name, Namespace: alert.Namespace},
			})
		}
	}
	return requests
}
