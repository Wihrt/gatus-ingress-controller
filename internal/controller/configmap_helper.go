package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// upsertConfigMapKey updates a single key in the target ConfigMap.
// The ConfigMap must already exist — the controller never creates it.
// If it does not exist yet, the reconciliation is requeued after 10 seconds.
func upsertConfigMapKey(ctx context.Context, c client.Client, namespace, cmName, key, value string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, cm)
	if errors.IsNotFound(err) {
		logger.Info("ConfigMap not found, requeueing", "name", cmName, "namespace", namespace)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, cmName, err)
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[key] = value
	if err := c.Update(ctx, cm); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ConfigMap %s/%s: %w", namespace, cmName, err)
	}
	logger.Info("Updated ConfigMap", "name", cmName, "namespace", namespace, "key", key)
	return ctrl.Result{}, nil
}
