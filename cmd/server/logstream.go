package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ── Live log stream ──────────────────────────────────────────
//
// The dashboard used to poll /api/logs every 5s, pulling 500 rows whether or not
// anything changed. This pushes each row as it is written instead: idle costs
// nothing, and rows appear as soon as the request finishes.
//
// Publish is called from logProxyRequestSplit, which runs ON THE REQUEST PATH of
// every proxied AI call. It must therefore never block: a stalled browser tab or
// a sleeping laptop must not be able to hold up a user's completion. Every send
// is non-blocking and drops on a full buffer.

const (
	// logStreamBuffer is the per-subscriber queue depth. A subscriber that falls
	// this far behind starts losing rows rather than applying backpressure to the
	// proxy. The client refetches on reconnect, so drops are recoverable.
	logStreamBuffer = 64

	// maxLogStreamSubscribers caps concurrent SSE connections. Each one holds a
	// goroutine and a socket open indefinitely, so an unbounded count is a cheap
	// resource-exhaustion vector.
	maxLogStreamSubscribers = 8

	// logStreamHeartbeat keeps idle connections alive through proxies that close
	// silent sockets, and lets the client notice a dead server.
	logStreamHeartbeat = 25 * time.Second
)

type logStreamHub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}

	// dropped counts rows discarded because a subscriber's buffer was full.
	// Surfaced in logs so a persistently lagging client is diagnosable rather
	// than silently losing data.
	dropped int
}

var logStream = &logStreamHub{subs: make(map[chan []byte]struct{})}

// subscribe registers a new listener. ok is false when the subscriber cap is
// reached, in which case the caller must reject the request.
func (h *logStreamHub) subscribe() (ch chan []byte, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= maxLogStreamSubscribers {
		return nil, false
	}
	ch = make(chan []byte, logStreamBuffer)
	h.subs[ch] = struct{}{}
	return ch, true
}

func (h *logStreamHub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.subs[ch]; exists {
		delete(h.subs, ch)
		close(ch)
	}
}

// publish fans a payload out to every subscriber without blocking. A subscriber
// whose buffer is full loses this row; it never delays the caller.
func (h *logStreamHub) publish(payload []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- payload:
		default:
			h.dropped++
			if h.dropped%100 == 1 {
				log.Printf("[PAAP] log stream: subscriber lagging, dropped %d rows total", h.dropped)
			}
		}
	}
}

func (h *logStreamHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publishLogRow serializes a freshly inserted logs row and broadcasts it. Field
// names match the objects returned by logList so the client can prepend the row
// into its existing cache without a second shape to handle.
//
// Called after the INSERT succeeds. Never returns an error: a broadcast failure
// must not affect the proxied request.
func publishLogRow(id int64, providerID, providerName, modelID, keyID, keyName,
	groupName, proxyUsed string, statusCode int, t tokenCounts, latencyMs int64,
	cost float64, errMsg, toolUsed, originalModel, pricingSource string, estimated bool) {

	if logStream.subscriberCount() == 0 {
		return
	}

	row := map[string]interface{}{
		"id": id,
		// Matches the DB's CURRENT_TIMESTAMP format ("YYYY-MM-DD HH:MM:SS", UTC)
		// so client-side date parsing and sorting behave identically to a refetch.
		"timestamp":          time.Now().UTC().Format("2006-01-02 15:04:05"),
		"provider_id":        providerID,
		"provider_name":      providerName,
		"model_id":           modelID,
		"key_id":             keyID,
		"key_name":           keyName,
		"group_name":         groupName,
		"framework":          "openai",
		"status_code":        statusCode,
		"race_status":        "",
		"race_id":            "",
		"tokens_in":          t.TotalIn(),
		"tokens_out":         t.TotalOut(),
		"latency_ms":         latencyMs,
		"cost_usd":           cost,
		"compression_ratio":  0,
		"skills_used":        "[]",
		"error":              errMsg,
		"proxy_used":         proxyUsed,
		"tool_used":          toolUsed,
		"original_model":     originalModel,
		"tokens_in_fresh":    t.InFresh,
		"tokens_cache_read":  t.CacheRead,
		"tokens_cache_write": t.CacheWrite,
		"tokens_reasoning":   t.Reasoning,
		"tokens_estimated":   estimated,
		"pricing_source":     pricingSource,
	}

	payload, err := json.Marshal(row)
	if err != nil {
		log.Printf("[PAAP] log stream: marshal failed: %v", err)
		return
	}
	logStream.publish(payload)
}

// logStreamHandler serves GET /api/logs/stream as Server-Sent Events.
//
// Auth is handled upstream by apiGuard (loopback or a valid gateway key), the
// same gate as every other /api route. This endpoint exposes key names, model
// names and error text, so it must never be mounted outside that guard.
func logStreamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming unsupported by this server")
		return
	}

	ch, ok := logStream.subscribe()
	if !ok {
		writeError(w, 503, fmt.Sprintf("log stream at capacity (%d subscribers)", maxLogStreamSubscribers))
		return
	}
	defer logStream.unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Defeats proxy response buffering, which would otherwise batch events and
	// defeat the point of streaming.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	// Tell the client it is connected before any row arrives, so the UI can show
	// live state on an idle server instead of waiting for traffic.
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(logStreamHeartbeat)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case payload, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-heartbeat.C:
			// SSE comment: ignored by EventSource, keeps the socket warm.
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
