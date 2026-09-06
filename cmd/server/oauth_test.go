package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestIsCodexOAuthProviderID(t *testing.T) {
	for _, providerID := range []string{"openai-codex", "builtin-openai-codex"} {
		if !isCodexOAuthProviderID(providerID) {
			t.Fatalf("%q must route to Codex OAuth", providerID)
		}
	}
	if isCodexOAuthProviderID("builtin-grok-cli") {
		t.Fatal("Grok must not route to Codex OAuth")
	}
}

func TestNewCodexTokenExchangeRequestMatchesCodexClientContract(t *testing.T) {
	req, err := newCodexTokenExchangeRequest("authorization-code", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	if req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("content type = %q", req.Header.Get("Content-Type"))
	}
	if req.Header.Get("Accept") != "application/json" || !strings.HasPrefix(req.Header.Get("User-Agent"), "codex_cli_rs/") {
		t.Fatalf("missing Codex headers: %#v", req.Header)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	values := string(body)
	for _, field := range []string{"grant_type=authorization_code", "code=authorization-code", "code_verifier=verifier", "client_id=" + codexClientID, "redirect_uri=https%3A%2F%2Fauth.openai.com%2Fdeviceauth%2Fcallback"} {
		if !strings.Contains(values, field) {
			t.Fatalf("missing %q in %q", field, values)
		}
	}
}

func TestMarshalCodexOAuthDataEscapesExternalValues(t *testing.T) {
	raw, err := marshalCodexOAuthData(`device"id`, `user\\code`, `2026-08-17T00:00:00Z`)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["device_auth_id"] != `device"id` || got["user_code"] != `user\\code` {
		t.Errorf("stored values = %#v", got)
	}
}

func TestIsCodebuddyProviderID(t *testing.T) {
	for _, providerID := range []string{"codebuddy", "builtin-codebuddy"} {
		if !isCodebuddyProviderID(providerID) {
			t.Fatalf("%q must route to CodeBuddy OAuth", providerID)
		}
	}
	for _, providerID := range []string{"openai-codex", "builtin-openai-codex", "builtin-grok-cli", "builtin-anigravity"} {
		if isCodebuddyProviderID(providerID) {
			t.Fatalf("%q must NOT route to CodeBuddy OAuth", providerID)
		}
	}
}

func TestCodebuddyFingerprintHeaders(t *testing.T) {
	h := codebuddyFingerprintHeaders()
	required := []string{"User-Agent", "X-Product", "X-IDE-Type", "X-IDE-Name", "X-IDE-Version", "X-Domain"}
	for _, k := range required {
		if v, ok := h[k]; !ok || v == "" {
			t.Errorf("missing/empty header %q", k)
		}
	}
	if v := h["X-Product"]; v != "CodeBuddy" {
		t.Errorf("X-Product = %q, want CodeBuddy", v)
	}
}

func TestCodebuddyNoAuthHeaders(t *testing.T) {
	h := codebuddyNoAuthHeaders()
	for _, k := range []string{"X-No-Authorization", "X-No-User-Id", "X-No-Enterprise-Id", "X-No-Department-Info"} {
		if h[k] != "true" {
			t.Errorf("%q = %q, want true", k, h[k])
		}
	}
	// Fingerprint headers must still be present on no-auth endpoints.
	if h["User-Agent"] == "" {
		t.Error("fingerprint User-Agent must be present on no-auth endpoints too")
	}
}
