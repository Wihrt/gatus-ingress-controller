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

// upsertSecretKey updates a single key in the target Secret.
// The Secret must already exist — the controller never creates it.
// If it does not exist yet, the reconciliation is requeued after 10 seconds.
func upsertSecretKey(ctx context.Context, c client.Client, namespace, secretName, key, value string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret)
	if errors.IsNotFound(err) {
		logger.Info("Secret not found, requeueing", "name", secretName, "namespace", namespace)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Secret %s/%s: %w", namespace, secretName, err)
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[key] = []byte(value)
	if err := c.Update(ctx, secret); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update Secret %s/%s: %w", namespace, secretName, err)
	}
	logger.Info("Updated Secret", "name", secretName, "namespace", namespace, "key", key)
	return ctrl.Result{}, nil
}
