package main

import "testing"

func TestValidateRuntimeProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "allowed prod colab host",
			raw:  "https://8080-gpu-abc.prod.colab.dev",
			want: "https://8080-gpu-abc.prod.colab.dev",
		},
		{
			name: "allowed googleusercontent host with trailing slash",
			raw:  "https://foo.colab.googleusercontent.com/",
			want: "https://foo.colab.googleusercontent.com",
		},
		{
			name:    "reject non https",
			raw:     "http://8080-gpu-abc.prod.colab.dev",
			wantErr: true,
		},
		{
			name:    "reject ip host",
			raw:     "https://127.0.0.1",
			wantErr: true,
		},
		{
			name:    "reject unexpected host",
			raw:     "https://evil.example.com",
			wantErr: true,
		},
		{
			name:    "reject unexpected path",
			raw:     "https://8080-gpu-abc.prod.colab.dev/api/sessions",
			wantErr: true,
		},
		{
			name:    "reject unexpected port",
			raw:     "https://8080-gpu-abc.prod.colab.dev:8443",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateRuntimeProxyURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRuntimeProxyURL(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("validateRuntimeProxyURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestHasAllowedRuntimeProxyHost(t *testing.T) {
	if !hasAllowedRuntimeProxyHost("8080-gpu-abc.prod.colab.dev") {
		t.Fatal("expected prod.colab.dev subdomain to be allowlisted")
	}
	if hasAllowedRuntimeProxyHost("prod.colab.dev.evil.example.com") {
		t.Fatal("unexpected host matched allowlist")
	}
}
