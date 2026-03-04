package main

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
	"github.com/Wihrt/gatus-ingress-controller/internal/controller"
	"github.com/Wihrt/gatus-ingress-controller/internal/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(monitoringv1alpha1.AddToScheme(scheme))
}

func main() {
	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	targetNamespace := os.Getenv("TARGET_NAMESPACE")
	if targetNamespace == "" {
		targetNamespace = "gatus"
	}

	secretName := os.Getenv("SECRET_NAME")
	if secretName == "" {
		secretName = "gatus-secrets"
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: ":8080",
		},
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				// Restrict Secret caching to only the target namespace,
				// avoiding cluster-wide list/watch on secrets.
				&corev1.Secret{}: {
					Namespaces: map[string]cache.Config{
						targetNamespace: {},
					},
				},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controller.GatusEndpointReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		TargetNamespace: targetNamespace,
		SecretName:      secretName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatusEndpoint")
		os.Exit(1)
	}

	if err = (&controller.GatusExternalEndpointReconciler{
		Client:          mgr.GetClient(),
		TargetNamespace: targetNamespace,
		SecretName:      secretName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatusExternalEndpoint")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&monitoringv1alpha1.GatusEndpoint{}).
		WithValidator(&webhook.GatusEndpointValidator{}).
		Complete(); err != nil {
		setupLog.Error(err, "unable to register webhook", "webhook", "GatusEndpoint")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
