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
