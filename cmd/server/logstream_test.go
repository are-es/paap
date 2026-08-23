package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// drainHub removes any subscribers left behind by a failed test so cases do not
// leak capacity into each other.
func drainHub(t *testing.T) {
	t.Helper()
	logStream.mu.Lock()
	for ch := range logStream.subs {
		delete(logStream.subs, ch)
		close(ch)
	}
	logStream.dropped = 0
	logStream.mu.Unlock()
}

// TestPublishDoesNotBlockOnFullBuffer is the critical safety property: publish is
// called on the AI request path, so a subscriber that never reads must not be
// able to stall it.
func TestPublishDoesNotBlockOnFullBuffer(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	ch, ok := logStream.subscribe()
	if !ok {
		t.Fatal("subscribe failed on an empty hub")
	}

	// Fill the buffer, then push far past it without ever reading.
	done := make(chan struct{})
	go func() {
		for i := 0; i < logStreamBuffer*10; i++ {
			logStream.publish([]byte(`{"id":1}`))
		}
		close(done)
	}()

	select {
	case <-done:
		// Expected: every publish returned immediately.
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a full subscriber buffer — this would stall proxied requests")
	}

	if len(ch) != logStreamBuffer {
		t.Errorf("buffer holds %d rows, want %d (capacity)", len(ch), logStreamBuffer)
	}
	logStream.mu.Lock()
	dropped := logStream.dropped
	logStream.mu.Unlock()
	if dropped == 0 {
		t.Error("dropped counter never incremented; overflow was not recorded")
	}
}

// TestSubscriberCapEnforced asserts the connection cap rejects excess clients
// rather than growing unbounded.
func TestSubscriberCapEnforced(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	var chans []chan []byte
	for i := 0; i < maxLogStreamSubscribers; i++ {
		ch, ok := logStream.subscribe()
		if !ok {
			t.Fatalf("subscribe %d/%d rejected below the cap", i+1, maxLogStreamSubscribers)
		}
		chans = append(chans, ch)
	}

	if _, ok := logStream.subscribe(); ok {
		t.Error("subscribe succeeded past the cap")
	}

	// Freeing one slot must let a new subscriber in.
	logStream.unsubscribe(chans[0])
	if _, ok := logStream.subscribe(); !ok {
		t.Error("subscribe failed after a slot was freed")
	}
}

// TestUnsubscribeIsIdempotent guards the double-close panic: the handler
// unsubscribes via defer, and a hub drain may have already removed the channel.
func TestUnsubscribeIsIdempotent(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	ch, _ := logStream.subscribe()
	logStream.unsubscribe(ch)
	logStream.unsubscribe(ch) // must not panic on an already-closed channel
}

// TestPublishConcurrentWithSubscribeChurn exercises the hub under the real
// pattern: many requests publishing while dashboards connect and disconnect.
// Run with -race to catch lock mistakes.
func TestPublishConcurrentWithSubscribeChurn(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				logStream.publish([]byte(`{"id":2}`))
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if ch, ok := logStream.subscribe(); ok {
				// Read a little, then leave — mimics a tab being closed.
				select {
				case <-ch:
				default:
				}
				logStream.unsubscribe(ch)
			}
		}
	}()

	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()

	if n := logStream.subscriberCount(); n != 0 {
		t.Errorf("%d subscribers leaked after churn", n)
	}
}

// TestLogStreamHandlerEmitsReadyThenRow asserts the wire format: a ready event on
// connect (so the UI can show live state on an idle server), then log events.
func TestLogStreamHandlerEmitsReadyThenRow(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	srv := httptest.NewServer(http.HandlerFunc(logStreamHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if b := resp.Header.Get("X-Accel-Buffering"); b != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no (proxy buffering defeats streaming)", b)
	}

	reader := bufio.NewReader(resp.Body)
	readEvent := func() (event, data string) {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if event != "" || data != "" {
					return event, data
				}
			}
		}
	}

	if ev, _ := readEvent(); ev != "ready" {
		t.Fatalf("first event = %q, want ready", ev)
	}

	// Wait for the handler's subscription to land before publishing.
	deadline := time.Now().Add(2 * time.Second)
	for logStream.subscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if logStream.subscriberCount() == 0 {
		t.Fatal("handler never subscribed")
	}

	publishLogRow(42, "prov-justwoker", "Justwoker", "claude-opus-5", "k1", "key",
		"", "", 200, tokenCounts{InFresh: 1000, CacheRead: 9000, Out: 250, Reasoning: 50},
		1234, 0.0075, "", "", "", pricingSourceExact, false)

	ev, data := readEvent()
	if ev != "log" {
		t.Fatalf("second event = %q, want log", ev)
	}

	var row map[string]interface{}
	if err := json.Unmarshal([]byte(data), &row); err != nil {
		t.Fatalf("row is not valid JSON: %v (%s)", err, data)
	}

	// The payload must carry the same field names logList returns, so the client
	// can prepend it into the existing cache without a second shape.
	for _, field := range []string{
		"id", "timestamp", "provider_name", "model_id", "status_code",
		"tokens_in", "tokens_out", "latency_ms", "cost_usd",
		"tokens_in_fresh", "tokens_cache_read", "tokens_reasoning",
		"tokens_estimated", "pricing_source",
	} {
		if _, present := row[field]; !present {
			t.Errorf("row is missing field %q that the logs table renders", field)
		}
	}

	// tokens_in must be the context-window sum, matching a refetch.
	if got := row["tokens_in"].(float64); got != 10000 {
		t.Errorf("tokens_in = %v, want 10000 (fresh + cache read)", got)
	}
	if got := row["tokens_out"].(float64); got != 300 {
		t.Errorf("tokens_out = %v, want 300 (out + reasoning)", got)
	}
	if got := row["pricing_source"].(string); got != pricingSourceExact {
		t.Errorf("pricing_source = %q, want %q", got, pricingSourceExact)
	}

	// The timestamp must parse in the same layout the DB emits, or the client's
	// date rendering diverges between a pushed row and a refetched one.
	ts := row["timestamp"].(string)
	if _, err := time.Parse("2006-01-02 15:04:05", ts); err != nil {
		t.Errorf("timestamp %q does not match the DB layout: %v", ts, err)
	}
}

// TestLogStreamHandlerRejectsAtCapacity asserts an over-capacity client gets a
// clean 503 instead of an accepted-but-dead connection.
func TestLogStreamHandlerRejectsAtCapacity(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	for i := 0; i < maxLogStreamSubscribers; i++ {
		if _, ok := logStream.subscribe(); !ok {
			t.Fatalf("failed to fill slot %d", i)
		}
	}

	w := httptest.NewRecorder()
	logStreamHandler(w, httptest.NewRequest("GET", "/api/logs/stream", nil))

	if w.Code != 503 {
		t.Errorf("status = %d, want 503 when at capacity", w.Code)
	}
}

// TestLogStreamHandlerRejectsNonGET keeps the endpoint read-only.
func TestLogStreamHandlerRejectsNonGET(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	w := httptest.NewRecorder()
	logStreamHandler(w, httptest.NewRequest("POST", "/api/logs/stream", nil))
	if w.Code != 405 {
		t.Errorf("status = %d, want 405 for POST", w.Code)
	}
}

// TestPublishLogRowSkipsWorkWithNoSubscribers asserts the zero-subscriber case
// costs nothing — this runs on every proxied request.
func TestPublishLogRowSkipsWorkWithNoSubscribers(t *testing.T) {
	drainHub(t)
	defer drainHub(t)

	if n := logStream.subscriberCount(); n != 0 {
		t.Fatalf("expected an empty hub, got %d subscribers", n)
	}
	// Must not panic and must not touch the dropped counter.
	publishLogRow(1, "p", "P", "m", "k", "K", "", "", 200,
		tokenCounts{InFresh: 1}, 1, 0, "", "", "", pricingSourceExact, false)

	logStream.mu.Lock()
	dropped := logStream.dropped
	logStream.mu.Unlock()
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 with no subscribers", dropped)
	}
}

// TestLoggingAProxyRequestPushesToStream is the end-to-end check: writing a log
// through the real writer must reach a live subscriber.
func TestLoggingAProxyRequestPushesToStream(t *testing.T) {
	setupPricingDB(t)
	drainHub(t)
	defer drainHub(t)

	ch, ok := logStream.subscribe()
	if !ok {
		t.Fatal("subscribe failed")
	}

	logProxyRequestSplit("prov-justwoker", "Justwoker", "claude-opus-5", "k1", "key",
		"", "", 200, tokenCounts{InFresh: 5000, CacheRead: 45000, Out: 800},
		910, "", nil, "", "", 0, 0)

	select {
	case payload := <-ch:
		var row map[string]interface{}
		if err := json.Unmarshal(payload, &row); err != nil {
			t.Fatalf("bad payload: %v", err)
		}
		if row["id"].(float64) <= 0 {
			t.Error("row id is not the real inserted rowid")
		}
		if got := row["tokens_cache_read"].(float64); got != 45000 {
			t.Errorf("tokens_cache_read = %v, want 45000", got)
		}
		// Cost must be the corrected cache-aware figure, not a flat-rate guess.
		if cost := row["cost_usd"].(float64); cost <= 0 {
			t.Errorf("cost_usd = %v, want a positive priced value", cost)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("logProxyRequestSplit did not push a row to the stream")
	}
}
