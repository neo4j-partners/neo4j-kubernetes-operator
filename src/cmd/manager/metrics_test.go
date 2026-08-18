package main

import (
	"net/http"
	"testing"
)

func TestMetricsServerOptions(t *testing.T) {
	off, err := metricsServerOptions("0", false)
	if err != nil || off.BindAddress != "0" || off.FilterProvider != nil {
		t.Fatalf("disabled: %#v err=%v", off, err)
	}

	empty, err := metricsServerOptions("", true)
	if err != nil || empty.BindAddress != "0" || empty.FilterProvider != nil {
		t.Fatalf("empty bind must stay disabled (controller-runtime would default :8080): %#v err=%v", empty, err)
	}

	if _, err := metricsServerOptions(":8080", false); err == nil {
		t.Fatal("plaintext metrics bind should error (NEO-017)")
	}
	if _, err := metricsServerOptions(":8443", false); err == nil {
		t.Fatal("insecure HTTPS-off bind should error (NEO-017)")
	}

	on, err := metricsServerOptions(":8443", true)
	if err != nil {
		t.Fatal(err)
	}
	if !on.SecureServing || on.FilterProvider == nil || on.BindAddress != ":8443" {
		t.Fatalf("enabled: %#v", on)
	}
}

func TestBearerToken(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	if _, ok := bearerToken(req); ok {
		t.Fatal("missing header")
	}
	req.Header.Set("Authorization", "Basic abc")
	if _, ok := bearerToken(req); ok {
		t.Fatal("non-bearer")
	}
	req.Header.Set("Authorization", "Bearer   ")
	if _, ok := bearerToken(req); ok {
		t.Fatal("empty token")
	}
	req.Header.Set("Authorization", "Bearer s3cret")
	got, ok := bearerToken(req)
	if !ok || got != "s3cret" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
