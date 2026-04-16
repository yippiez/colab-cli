package main

import "testing"

func TestStartOutput_ExcludesSensitiveProxyMetadata(t *testing.T) {
	out := startOutput(&Runtime{
		Endpoint:    "endpoint-123",
		Accelerator: "T4",
		ProxyURL:    "https://8080-gpu-abc.prod.colab.dev",
	})

	if got := out["session"]; got != "endpoint-123" {
		t.Fatalf("session = %v, want endpoint-123", got)
	}
	if _, ok := out["proxyUrl"]; ok {
		t.Fatal("startOutput unexpectedly exposed proxyUrl")
	}
	if _, ok := out["endpoint"]; ok {
		t.Fatal("startOutput unexpectedly exposed endpoint")
	}
}

func TestBasicStatusOutput_ExcludesEndpoint(t *testing.T) {
	out := basicStatusOutput(&Runtime{
		Endpoint:    "endpoint-123",
		Accelerator: "T4",
	})

	if got := out["gpu"]; got != "T4" {
		t.Fatalf("gpu = %v, want T4", got)
	}
	if _, ok := out["endpoint"]; ok {
		t.Fatal("basicStatusOutput unexpectedly exposed endpoint")
	}
	if _, ok := out["proxyUrl"]; ok {
		t.Fatal("basicStatusOutput unexpectedly exposed proxyUrl")
	}
}
