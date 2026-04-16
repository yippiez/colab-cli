package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestRunLogout_RevokesRefreshTokenAndClearsCache(t *testing.T) {
	tempHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("Setenv HOME: %v", err)
	}
	defer os.Setenv("HOME", oldHome)

	if err := saveToken(&oauth2.Token{AccessToken: "access-token", RefreshToken: "refresh-token"}); err != nil {
		t.Fatalf("saveToken() error = %v", err)
	}

	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Fatalf("Content-Type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		gotToken = r.Form.Get("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldRevokeURL := googleRevokeURL
	googleRevokeURL = server.URL
	defer func() { googleRevokeURL = oldRevokeURL }()

	if err := runLogout([]string{"--json"}); err != nil {
		t.Fatalf("runLogout() error = %v", err)
	}

	if gotToken != "refresh-token" {
		t.Fatalf("revoked token = %q, want refresh-token", gotToken)
	}
	path, err := tokenCachePath()
	if err != nil {
		t.Fatalf("tokenCachePath() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token cache still exists at %s", path)
	}
}

func TestRunLogout_LeavesCacheWhenRevokeFails(t *testing.T) {
	tempHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("Setenv HOME: %v", err)
	}
	defer os.Setenv("HOME", oldHome)

	cached := &oauth2.Token{AccessToken: "access-token", RefreshToken: "refresh-token"}
	if err := saveToken(cached); err != nil {
		t.Fatalf("saveToken() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	oldRevokeURL := googleRevokeURL
	googleRevokeURL = server.URL
	defer func() { googleRevokeURL = oldRevokeURL }()

	err := runLogout(nil)
	if err == nil {
		t.Fatal("runLogout() error = nil, want error")
	}

	path, err := tokenCachePath()
	if err != nil {
		t.Fatalf("tokenCachePath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var got oauth2.Token
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal cached token: %v", err)
	}
	if got.RefreshToken != cached.RefreshToken {
		t.Fatalf("cached refresh token = %q, want %q", got.RefreshToken, cached.RefreshToken)
	}
}

func TestRevokeToken_UsesAccessTokenWhenRefreshTokenMissing(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		gotToken = r.Form.Get("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldRevokeURL := googleRevokeURL
	googleRevokeURL = server.URL
	defer func() { googleRevokeURL = oldRevokeURL }()

	if err := revokeToken(context.Background(), &oauth2.Token{AccessToken: "access-only"}); err != nil {
		t.Fatalf("revokeToken() error = %v", err)
	}
	if gotToken != "access-only" {
		t.Fatalf("revoked token = %q, want access-only", gotToken)
	}
}

func TestClearTokenCache_IgnoresMissingFile(t *testing.T) {
	tempHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("Setenv HOME: %v", err)
	}
	defer os.Setenv("HOME", oldHome)

	if err := clearTokenCache(); err != nil {
		t.Fatalf("clearTokenCache() error = %v", err)
	}
}

func TestRevokeToken_SendsFormEncodedRequest(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldRevokeURL := googleRevokeURL
	googleRevokeURL = server.URL
	defer func() { googleRevokeURL = oldRevokeURL }()

	if err := revokeToken(context.Background(), &oauth2.Token{RefreshToken: "hello world"}); err != nil {
		t.Fatalf("revokeToken() error = %v", err)
	}
	if gotBody != (url.Values{"token": []string{"hello world"}}).Encode() {
		t.Fatalf("request body = %q", gotBody)
	}
}

func TestTokenCachePath_UsesHomeDirectory(t *testing.T) {
	tempHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("Setenv HOME: %v", err)
	}
	defer os.Setenv("HOME", oldHome)

	got, err := tokenCachePath()
	if err != nil {
		t.Fatalf("tokenCachePath() error = %v", err)
	}
	want := filepath.Join(tempHome, ".config", "colab", "token.json")
	if got != want {
		t.Fatalf("tokenCachePath() = %q, want %q", got, want)
	}
}
