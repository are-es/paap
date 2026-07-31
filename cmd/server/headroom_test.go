package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// resetHeadroomHealth clears the cached /health verdict so each case probes fresh.
// Also clears the compression cache so sub-tests don't hit each other's cached output.
func resetHeadroomHealth() {
	headroomHealthMu.Lock()
	headroomHealthy = false
	headroomHealthKnown = false
	headroomHealthExpires = time.Time{}
	headroomHealthMu.Unlock()

	hrCacheStore.mu.Lock()
	hrCacheStore.by = map[string]hrCacheSlot{}
	hrCacheStore.lru = nil
	hrCacheStore.bytes = 0
	hrCacheStore.mu.Unlock()
}

func bigToolMsg(content string) map[string]interface{} {
	return map[string]interface{}{"role": "tool", "content": content}
}

func pad(n int) string { return strings.Repeat("x", n) }

// headroomFake serves /health 200 and /v1/compress with the supplied handler.
// hits counts compress calls so passthrough cases can assert zero HTTP traffic.
func headroomFake(t *testing.T, hits *int32, compress http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/compress", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		compress(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// echoCompress returns the request messages with every tool content replaced by short.
func echoCompress(short string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		for _, m := range req.Messages {
			if role, _ := m["role"].(string); role == "tool" {
				if _, ok := m["content"].(string); ok {
					m["content"] = short
				}
			}
		}
		json.NewEncoder(w).Encode(headroomResponse{Messages: req.Messages})
	}
}

func setHeadroom(t *testing.T, enabled, url, timeoutMS string) {
	t.Helper()
	setSetting("headroom_enabled", enabled)
	setSetting("headroom_url", url)
	setSetting("headroom_timeout_ms", timeoutMS)
	t.Cleanup(func() { setSetting("headroom_enabled", "false") })
}

func TestCompressEndpoint(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"http://127.0.0.1:8787", "http://127.0.0.1:8787/v1/compress"},
		{"http://127.0.0.1:8787/", "http://127.0.0.1:8787/v1/compress"},
		{"http://h/base///", "http://h/base/v1/compress"},
	} {
		if got := compressEndpoint(tt.in); got != tt.want {
			t.Errorf("compressEndpoint(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHeadroomPassthrough(t *testing.T) {
	big := pad(headroomMinCompressSize + 100)

	tests := []struct {
		name     string
		enabled  string
		badURL   bool // point at a dead port instead of the fake
		messages []map[string]interface{}
		wantHits int32
	}{
		{
			name:     "disabled setting makes no HTTP call",
			enabled:  "false",
			messages: []map[string]interface{}{bigToolMsg(big)},
		},
		{
			name:     "health check fails",
			enabled:  "true",
			badURL:   true,
			messages: []map[string]interface{}{bigToolMsg(big)},
		},
		{
			name:     "all candidates too small",
			enabled:  "true",
			messages: []map[string]interface{}{bigToolMsg("tiny"), {"role": "user", "content": big}},
		},
		{
			name:    "error tool result is not a candidate",
			enabled: "true",
			messages: []map[string]interface{}{
				{"role": "tool", "content": big, "is_error": true},
				{"role": "tool", "content": big, "status": "error"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetHeadroomHealth()
			var hits int32
			srv := headroomFake(t, &hits, echoCompress("short"))
			url := srv.URL
			if tt.badURL {
				url = "http://127.0.0.1:1" // nothing listens here
			}
			setHeadroom(t, tt.enabled, url, "3000")

			got := CompressWithHeadroom(tt.messages, "gpt-4o")
			for i := range tt.messages {
				if got[i]["content"] != tt.messages[i]["content"] {
					t.Errorf("message %d was modified", i)
				}
			}
			if n := atomic.LoadInt32(&hits); n != tt.wantHits {
				t.Errorf("compress calls = %d, want %d", n, tt.wantHits)
			}
		})
	}
}

func TestHeadroomHappyPath(t *testing.T) {
	resetHeadroomHealth()
	var hits int32
	srv := headroomFake(t, &hits, echoCompress("compressed"))
	setHeadroom(t, "true", srv.URL, "3000")

	big := pad(headroomMinCompressSize + 100)
	sys := "SYSTEM PROMPT PAAP OWNS"
	in := []map[string]interface{}{
		{"role": "system", "content": sys},
		{"role": "user", "content": "do the thing"},
		bigToolMsg(big),
	}
	got := CompressWithHeadroom(in, "gpt-4o")

	if got[2]["content"] != "compressed" {
		t.Errorf("tool content = %v, want compressed", got[2]["content"])
	}
	if got[0]["content"] != sys {
		t.Errorf("system message was modified: %v", got[0]["content"])
	}
	if got[1]["content"] != "do the thing" {
		t.Errorf("user message was modified: %v", got[1]["content"])
	}
	if in[2]["content"] != big {
		t.Error("input slice was mutated in place")
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("compress calls = %d, want 1", n)
	}
}

func TestHeadroomBadResponses(t *testing.T) {
	big := pad(headroomMinCompressSize + 100)
	in := []map[string]interface{}{
		{"role": "user", "content": "q"},
		bigToolMsg(big),
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "message count mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(headroomResponse{Messages: []map[string]interface{}{
					{"role": "tool", "content": "short"},
				}})
			},
		},
		{
			name: "role order mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(headroomResponse{Messages: []map[string]interface{}{
					{"role": "tool", "content": "short"},
					{"role": "user", "content": "q"},
				}})
			},
		},
		{
			name:    "phantom savings",
			handler: echoCompress(big), // same size back
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
			},
		},
		{
			name: "timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(300 * time.Millisecond)
				echoCompress("short")(w, r)
			},
		},
		{
			name: "malformed json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("{not json"))
			},
		},
		{
			name: "missing messages field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"tokens_saved": 999}`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetHeadroomHealth()
			var hits int32
			srv := headroomFake(t, &hits, tt.handler)
			timeout := "3000"
			if tt.name == "timeout" {
				timeout = "50"
			}
			setHeadroom(t, "true", srv.URL, timeout)

			got := CompressWithHeadroom(in, "gpt-4o")
			for i := range in {
				if got[i]["content"] != in[i]["content"] {
					t.Errorf("message %d changed to %v, want original", i, got[i]["content"])
				}
			}
		})
	}
}
