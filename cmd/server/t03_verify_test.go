package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dolvin/paap/internal/db"
)

func TestMain(m *testing.M) {
	dir := filepath.Join(os.TempDir(), "paap-test-t03")
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)
	db.Init(dir)
	code := m.Run()
	db.Close()
	os.Exit(code)
}

func TestProviderRoutes_ModelDetect(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
	}{
		{"POST models/detect spec route", "/api/providers/test-provider/models/detect", "POST", 404},
		{"POST detect-models legacy route", "/api/providers/test-provider/detect-models", "POST", 404},
		{"GET models/detect wrong method", "/api/providers/test-provider/models/detect", "GET", 405},
		{"POST playground alias", "/api/providers/test-provider/playground", "POST", 400},
		{"POST test-prompt-stream", "/api/providers/test-provider/test-prompt-stream", "POST", 400},
		{"GET connections", "/api/providers/test-provider/connections", "GET", 200},
		{"POST connections", "/api/providers/test-provider/connections", "POST", 404},
		{"PUT round-robin", "/api/providers/test-provider/round-robin", "PUT", 400},
		{"DELETE builtin provider", "/api/providers/builtin-xiaomi", "DELETE", 403},
		{"GET provider keys", "/api/providers/test-provider/keys", "GET", 200},
		{"GET provider models", "/api/providers/test-provider/models", "GET", 200},
		{"GET providers list", "/api/providers", "GET", 200},
		{"POST providers create (no body)", "/api/providers", "POST", 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handler http.HandlerFunc
			if tt.path == "/api/providers" {
				handler = methodRouter(map[string]http.HandlerFunc{
					"GET":  providerList,
					"POST": providerCreate,
				})
			} else {
				handler = providerRoutes
			}
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			handler(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("got %d, want %d — body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
