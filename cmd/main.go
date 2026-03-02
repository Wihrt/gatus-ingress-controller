package main

import (
"os"
"strings"

networkingv1 "k8s.io/api/networking/v1"
"k8s.io/apimachinery/pkg/runtime"
utilruntime "k8s.io/apimachinery/pkg/util/runtime"
clientgoscheme "k8s.io/client-go/kubernetes/scheme"
ctrl "sigs.k8s.io/controller-runtime"
"sigs.k8s.io/controller-runtime/pkg/healthz"
"sigs.k8s.io/controller-runtime/pkg/log/zap"
metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
"github.com/Wihrt/gatus-ingress-controller/internal/controller"
)

var (
scheme   = runtime.NewScheme()
setupLog = ctrl.Log.WithName("setup")
)

func init() {
utilruntime.Must(clientgoscheme.AddToScheme(scheme))
utilruntime.Must(monitoringv1alpha1.AddToScheme(scheme))
utilruntime.Must(networkingv1.AddToScheme(scheme))
}

func main() {
opts := zap.Options{
Development: true,
}
ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
Scheme: scheme,
Metrics: metricsserver.Options{
BindAddress: ":8080",
},
HealthProbeBindAddress: ":8081",
LeaderElection:         false,
})
if err != nil {
setupLog.Error(err, "unable to start manager")
os.Exit(1)
}

ingressClass := os.Getenv("INGRESS_CLASS")
if ingressClass == "" {
ingressClass = "traefik"
}

targetNamespace := os.Getenv("TARGET_NAMESPACE")
if targetNamespace == "" {
targetNamespace = "gatus"
}

configMapName := os.Getenv("CONFIG_MAP_NAME")
if configMapName == "" {
configMapName = "gatus-config"
}

if err = (&controller.IngressReconciler{
Client:       mgr.GetClient(),
Scheme:       mgr.GetScheme(),
IngressClass: ingressClass,
}).SetupWithManager(mgr); err != nil {
setupLog.Error(err, "unable to create controller", "controller", "Ingress")
os.Exit(1)
}

	if err = (&controller.GatusAlertReconciler{
		Client:          mgr.GetClient(),
		TargetNamespace: targetNamespace,
		ConfigMapName:   configMapName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatusAlert")
		os.Exit(1)
	}

	if err = (&controller.GatusAnnouncementReconciler{
		Client:          mgr.GetClient(),
		TargetNamespace: targetNamespace,
		ConfigMapName:   configMapName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatusAnnouncement")
		os.Exit(1)
	}

	if err = (&controller.GatusMaintenanceReconciler{
		Client:          mgr.GetClient(),
		TargetNamespace: targetNamespace,
		ConfigMapName:   configMapName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatusMaintenance")
		os.Exit(1)
	}

if err = (&controller.GatusEndpointReconciler{
Client:          mgr.GetClient(),
Scheme:          mgr.GetScheme(),
TargetNamespace: targetNamespace,
ConfigMapName:   configMapName,
}).SetupWithManager(mgr); err != nil {
setupLog.Error(err, "unable to create controller", "controller", "GatusEndpoint")
os.Exit(1)
}

if err = (&controller.GatusExternalEndpointReconciler{
Client:          mgr.GetClient(),
TargetNamespace: targetNamespace,
ConfigMapName:   configMapName,
}).SetupWithManager(mgr); err != nil {
setupLog.Error(err, "unable to create controller", "controller", "GatusExternalEndpoint")
os.Exit(1)
}

// Gateway API (HTTPRoute) support is opt-in because the Gateway API CRDs are not
// installed in every Kubernetes cluster. Enable it by setting GATEWAY_API_ENABLED=true.
if os.Getenv("GATEWAY_API_ENABLED") == "true" {
setupLog.Info("Gateway API support enabled; registering HTTPRoute controller")
utilruntime.Must(gatewayv1.Install(scheme))

var gatewayNames []string
if raw := os.Getenv("GATEWAY_NAMES"); raw != "" {
for _, name := range strings.Split(raw, ",") {
name = strings.TrimSpace(name)
if name != "" {
gatewayNames = append(gatewayNames, name)
}
}
}

if err = (&controller.HTTPRouteReconciler{
Client:       mgr.GetClient(),
Scheme:       mgr.GetScheme(),
GatewayNames: gatewayNames,
}).SetupWithManager(mgr); err != nil {
setupLog.Error(err, "unable to create controller", "controller", "HTTPRoute")
os.Exit(1)
}
}

if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
setupLog.Error(err, "unable to set up health check")
os.Exit(1)
}
if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
setupLog.Error(err, "unable to set up ready check")
os.Exit(1)
}

setupLog.Info("starting manager")
if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
setupLog.Error(err, "problem running manager")
os.Exit(1)
}
}
