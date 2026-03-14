package controller

import (
	"encoding/json"
	"testing"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	monitoringv1alpha1 "github.com/Wihrt/gatus-ingress-controller/api/v1alpha1"
)

func TestBoolPtr(t *testing.T) {
	truePtr := boolPtr(true)
	if truePtr == nil || *truePtr != true {
		t.Error("boolPtr(true) should return pointer to true")
	}

	falsePtr := boolPtr(false)
	if falsePtr == nil || *falsePtr != false {
		t.Error("boolPtr(false) should return pointer to false")
	}

	// Verify they point to different memory locations.
	if truePtr == falsePtr {
		t.Error("boolPtr should return distinct pointers for different calls")
	}
}

func TestConvertAlerts_Empty(t *testing.T) {
	result := convertAlerts(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

func TestConvertAlerts_SingleAlert(t *testing.T) {
	trueVal := true
	alerts := []monitoringv1alpha1.GatusAlertSpec{
		{
			Type:             "slack",
			Enabled:          &trueVal,
			FailureThreshold: 3,
			SuccessThreshold: 2,
			Description:      "test alert",
		},
	}

	result := convertAlerts(alerts)
	if len(result) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(result))
	}

	a := result[0]
	if a.Type != "slack" {
		t.Errorf("type = %q, want 'slack'", a.Type)
	}
	if a.Enabled == nil || *a.Enabled != true {
		t.Error("enabled should be true")
	}
	if a.FailureThreshold != 3 {
		t.Errorf("failure-threshold = %d, want 3", a.FailureThreshold)
	}
	if a.SuccessThreshold != 2 {
		t.Errorf("success-threshold = %d, want 2", a.SuccessThreshold)
	}
	if a.Description != "test alert" {
		t.Errorf("description = %q, want 'test alert'", a.Description)
	}
}

func TestConvertAlerts_WithProviderOverride(t *testing.T) {
	webhookURL, _ := json.Marshal("https://hooks.slack.com/services/test")
	alerts := []monitoringv1alpha1.GatusAlertSpec{
		{
			Type: "slack",
			ProviderOverride: map[string]apiextv1.JSON{
				"webhook-url": {Raw: webhookURL},
			},
		},
	}

	result := convertAlerts(alerts)
	if len(result) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(result))
	}
	if result[0].ProviderOverride == nil {
		t.Fatal("expected provider-override to be set")
	}
	if result[0].ProviderOverride["webhook-url"] != "https://hooks.slack.com/services/test" {
		t.Errorf("webhook-url = %v, want 'https://hooks.slack.com/services/test'", result[0].ProviderOverride["webhook-url"])
	}
}

func TestConvertAlerts_MultipleAlerts(t *testing.T) {
	alerts := []monitoringv1alpha1.GatusAlertSpec{
		{Type: "slack"},
		{Type: "discord"},
		{Type: "pagerduty"},
	}

	result := convertAlerts(alerts)
	if len(result) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(result))
	}

	expectedTypes := []string{"slack", "discord", "pagerduty"}
	for i, expected := range expectedTypes {
		if result[i].Type != expected {
			t.Errorf("alert[%d].type = %q, want %q", i, result[i].Type, expected)
		}
	}
}

func TestApiExtMapToInterface_Empty(t *testing.T) {
	result := apiExtMapToInterface(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for nil input, got %d entries", len(result))
	}
}

func TestApiExtMapToInterface_StringValue(t *testing.T) {
	val, _ := json.Marshal("hello")
	m := map[string]apiextv1.JSON{
		"key": {Raw: val},
	}

	result := apiExtMapToInterface(m)
	if result["key"] != "hello" {
		t.Errorf("key = %v, want 'hello'", result["key"])
	}
}

func TestApiExtMapToInterface_NumericValue(t *testing.T) {
	val, _ := json.Marshal(42.0)
	m := map[string]apiextv1.JSON{
		"count": {Raw: val},
	}

	result := apiExtMapToInterface(m)
	if result["count"] != 42.0 {
		t.Errorf("count = %v, want 42.0", result["count"])
	}
}

func TestApiExtMapToInterface_InvalidJSON(t *testing.T) {
	m := map[string]apiextv1.JSON{
		"bad": {Raw: []byte("{invalid")},
	}

	result := apiExtMapToInterface(m)
	if _, ok := result["bad"]; ok {
		t.Error("expected invalid JSON entry to be skipped")
	}
}

func TestBuildClientYAML_FullConfig(t *testing.T) {
	c := &monitoringv1alpha1.GatusClientConfig{
		Insecure:       true,
		IgnoreRedirect: true,
		Timeout:        "30s",
		DNSResolver:    "tcp://8.8.8.8:53",
		ProxyURL:       "http://proxy:8080",
		Network:        "ip4",
		OAuth2: &monitoringv1alpha1.GatusClientOAuth2Config{
			TokenURL:     "https://auth.example.com/token",
			ClientID:     "my-id",
			ClientSecret: "my-secret",
			Scopes:       []string{"read", "write"},
		},
		TLS: &monitoringv1alpha1.GatusClientTLSConfig{
			CertificateFile: "/certs/tls.crt",
			PrivateKeyFile:  "/certs/tls.key",
			Renegotiation:   "never",
		},
	}

	result := buildClientYAML(c)

	if !result.Insecure {
		t.Error("insecure should be true")
	}
	if !result.IgnoreRedirect {
		t.Error("ignore-redirect should be true")
	}
	if result.Timeout != "30s" {
		t.Errorf("timeout = %q, want '30s'", result.Timeout)
	}
	if result.DNSResolver != "tcp://8.8.8.8:53" {
		t.Errorf("dns-resolver = %q, want 'tcp://8.8.8.8:53'", result.DNSResolver)
	}
	if result.ProxyURL != "http://proxy:8080" {
		t.Errorf("proxy-url = %q, want 'http://proxy:8080'", result.ProxyURL)
	}
	if result.Network != "ip4" {
		t.Errorf("network = %q, want 'ip4'", result.Network)
	}
	if result.OAuth2 == nil {
		t.Fatal("oauth2 should not be nil")
	}
	if result.OAuth2.TokenURL != "https://auth.example.com/token" {
		t.Errorf("oauth2.token-url = %q, want 'https://auth.example.com/token'", result.OAuth2.TokenURL)
	}
	if result.TLS == nil {
		t.Fatal("tls should not be nil")
	}
	if result.TLS.CertificateFile != "/certs/tls.crt" {
		t.Errorf("tls.certificate-file = %q, want '/certs/tls.crt'", result.TLS.CertificateFile)
	}
}

func TestBuildClientYAML_MinimalConfig(t *testing.T) {
	c := &monitoringv1alpha1.GatusClientConfig{
		Timeout: "10s",
	}

	result := buildClientYAML(c)

	if result.Timeout != "10s" {
		t.Errorf("timeout = %q, want '10s'", result.Timeout)
	}
	if result.OAuth2 != nil {
		t.Error("oauth2 should be nil when not configured")
	}
	if result.TLS != nil {
		t.Error("tls should be nil when not configured")
	}
}
