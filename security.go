package main

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
)

var allowedRuntimeProxyHostSuffixes = []string{
	".prod.colab.dev",
	".colab.googleusercontent.com",
	".colab.research.google.com",
}

func validateRuntimeProxyURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("missing runtime proxy URL")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse runtime proxy URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("unexpected runtime proxy scheme %q", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("runtime proxy URL must not include user info")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("runtime proxy URL must not include query or fragment")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("runtime proxy URL must not include path %q", u.Path)
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return "", fmt.Errorf("runtime proxy URL is missing a hostname")
	}
	if net.ParseIP(hostname) != nil {
		return "", fmt.Errorf("runtime proxy URL must not use an IP address")
	}
	if port := u.Port(); port != "" && port != "443" {
		return "", fmt.Errorf("unexpected runtime proxy port %q", port)
	}
	if !hasAllowedRuntimeProxyHost(hostname) {
		return "", fmt.Errorf("runtime proxy host %q is not allowlisted", hostname)
	}

	return (&url.URL{Scheme: "https", Host: u.Host}).String(), nil
}

func hasAllowedRuntimeProxyHost(hostname string) bool {
	for _, suffix := range allowedRuntimeProxyHostSuffixes {
		trimmed := strings.TrimPrefix(suffix, ".")
		if hostname == trimmed || strings.HasSuffix(hostname, suffix) {
			return true
		}
	}
	return false
}

func logRuntimeProxyValidationFailure(raw string, err error) {
	log.Printf("security: refusing runtime proxy URL %q: %v", raw, err)
}
