package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ── Request dump handle ────────────────────────────────────────────────────
// Accumulates request metadata in two phases: inbound (at handler entry) and
// upstream (before the outgoing http.NewRequest). Finish writes a single-line
// block to the shared requests.log via LogRequestDump.
// All methods tolerate a nil receiver so call sites need no nil checks.

const dumpPreviewLen = 200 // max prompt length in runes

type RequestDump struct {
	mu            sync.Mutex
	RequestID     string                 `json:"request_id"`
	Timestamp     string                 `json:"timestamp"`
	Method        string                 `json:"method"`
	Path          string                 `json:"path"`
	ClientKey     string                 `json:"client_key"`
	Model         string                 `json:"model"`
	Stream        bool                   `json:"stream"`
	Prompt        map[string]interface{} `json:"prompt"`
	MsgCount      int                    `json:"msg_count"`
	ToolCount     int                    `json:"tool_count"`
	InboundParams map[string]interface{} `json:"inbound_params"`
	Upstream      map[string]interface{} `json:"upstream,omitempty"`
	ParamDiff     map[string]interface{} `json:"param_diff,omitempty"`
	Response      map[string]interface{} `json:"response,omitempty"`

	rawInbound  map[string]interface{}
	rawUpstream map[string]interface{}
}

// BeginRequestDump creates a new dump handle from the parsed client body.
func BeginRequestDump(method, path, clientKey string, body map[string]interface{}) *RequestDump {
	if body == nil {
		return nil
	}
	d := &RequestDump{
		RequestID:  shortID(),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Method:     method,
		Path:       path,
		ClientKey:  clientKey,
		Model:      stringVal(body, "model"),
		Stream:     boolVal(body, "stream"),
		rawInbound: body,
	}
	d.extractPromptAndHistory(body)
	d.extractInboundParams(body)
	return d
}

// SetUpstream records the upstream provider, URL, and the body actually sent.
func (d *RequestDump) SetUpstream(provider, url string, body map[string]interface{}) {
	if d == nil || body == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Upstream = map[string]interface{}{
		"provider": provider,
		"url":      url,
		"params":   extractSamplingParams(body),
	}
	d.rawUpstream = body
}

// Finish computes param_diff and writes to the shared log. Idempotent.
func (d *RequestDump) Finish(status int, latencyMs int64, tokensIn, tokensOut int, usage map[string]interface{}) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.Response != nil {
		return // idempotent
	}

	d.Response = map[string]interface{}{
		"status":     status,
		"latency_ms": latencyMs,
		"tokens_in":  tokensIn,
		"tokens_out": tokensOut,
	}
	if len(usage) > 0 {
		d.Response["usage"] = usage
	}

	d.computeParamDiff()

	// Write synchronously — entries are ~400 bytes, microseconds under mutex
	LogRequestDump(d)
}

// FinishError records a failed request and writes it to the log. Idempotent,
// nil-safe, mutually exclusive with Finish.
func (d *RequestDump) FinishError(status int, latencyMs int64, errMsg string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.Response != nil {
		return // idempotent — first call wins
	}

	d.Response = map[string]interface{}{
		"status":     status,
		"latency_ms": latencyMs,
		"error":      errMsg,
	}

	d.computeParamDiff()

	// Write synchronously
	LogRequestDump(d)
}

// ── Extraction helpers ─────────────────────────────────────────────────────

func (d *RequestDump) extractPromptAndHistory(body map[string]interface{}) {
	msgs, ok := body["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		return
	}

	d.MsgCount = len(msgs)

	var lastUserIdx int = -1
	for i, m := range msgs {
		if mm, ok := m.(map[string]interface{}); ok {
			if mm["role"] == "user" {
				lastUserIdx = i
			}
		}
	}

	if lastUserIdx >= 0 {
		mm, ok := msgs[lastUserIdx].(map[string]interface{})
		if ok {
			role, _ := mm["role"].(string)
			content := extractTextContent(mm["content"])
			fullLen := utf8.RuneCountInString(content)

			// Truncate to dumpPreviewLen runes at extraction time
			truncated := content
			runes := []rune(content)
			if len(runes) > dumpPreviewLen {
				truncated = string(runes[:dumpPreviewLen])
			}

			d.Prompt = map[string]interface{}{
				"role":        role,
				"content":     truncated,
				"content_len": fullLen,
			}
		}
	}
}

func (d *RequestDump) extractInboundParams(body map[string]interface{}) {
	params := map[string]interface{}{}
	for k, v := range body {
		switch k {
		case "messages", "model", "stream":
			continue
		case "tools":
			if tools, ok := v.([]interface{}); ok {
				d.ToolCount = len(tools)
			}
		default:
			params[k] = v
		}
	}
	d.InboundParams = params
}

// ── Sampling param extraction & diff ───────────────────────────────────────

func extractSamplingParams(body map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	samplingKeys := []string{
		"temperature", "top_p", "topP", "top_k", "topK",
		"max_tokens", "maxOutputTokens", "max_completion_tokens",
		"presence_penalty", "frequency_penalty",
		"stop", "stopSequences", "stop_sequences",
		"seed", "reasoning_effort",
	}
	for _, k := range samplingKeys {
		if v, ok := body[k]; ok {
			out[k] = v
		}
	}
	if gc, ok := body["generationConfig"].(map[string]interface{}); ok {
		for _, k := range samplingKeys {
			if v, ok := gc[k]; ok {
				out[k] = v
			}
		}
	}
	return out
}

func (d *RequestDump) computeParamDiff() {
	inbound := extractSamplingParams(d.rawInbound)
	upstream := extractSamplingParams(d.rawUpstream)
	inNorm := normaliseParamKeys(inbound)
	upNorm := normaliseParamKeys(upstream)

	dropped := []string{}
	injected := map[string]interface{}{}
	changed := map[string]interface{}{}

	for canon, inVal := range inNorm {
		if upVal, ok := upNorm[canon]; ok {
			if !valuesEqual(inVal, upVal) {
				changed[canon] = map[string]interface{}{"client": inVal, "upstream": upVal}
			}
		} else {
			dropped = append(dropped, canon)
		}
	}
	for canon, upVal := range upNorm {
		if _, ok := inNorm[canon]; !ok {
			injected[canon] = upVal
		}
	}

	dropped = sortedStrings(dropped)
	d.ParamDiff = map[string]interface{}{
		"dropped":  dropped,
		"injected": injected,
		"changed":  changed,
	}
}

func normaliseParamKeys(params map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range params {
		out[canonicalParamName(k)] = v
	}
	return out
}

func canonicalParamName(k string) string {
	switch strings.ToLower(k) {
	case "temperature":
		return "temperature"
	case "top_p", "topp":
		return "top_p"
	case "top_k", "topk":
		return "top_k"
	case "max_tokens", "maxoutputtokens", "max_completion_tokens":
		return "max_tokens"
	case "presence_penalty":
		return "presence_penalty"
	case "frequency_penalty":
		return "frequency_penalty"
	case "stop", "stopsequences", "stop_sequences":
		return "stop"
	case "seed":
		return "seed"
	case "reasoning_effort":
		return "reasoning_effort"
	default:
		return strings.ToLower(k)
	}
}

func valuesEqual(a, b interface{}) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func sortedStrings(s []string) []string {
	sort.Strings(s)
	return s
}

// ── Client sampling param pass-through ─────────────────────────────────────
// Transparent: PAAP copies only what the client actually sent.
// Absent params stay absent so the provider's own default applies.

type paramMapping struct {
	openai string
	target string
}

func applyClientSampling(rawBody map[string]interface{}, target map[string]interface{}, mappings []paramMapping) map[string]interface{} {
	for _, m := range mappings {
		if clientVal, ok := findParam(rawBody, m.openai); ok {
			target[m.target] = clientVal
		}
	}
	return target
}

func findParam(body map[string]interface{}, name string) (interface{}, bool) {
	if v, ok := body[name]; ok {
		return v, true
	}
	camel := snakeToCamel(name)
	if camel != name {
		if v, ok := body[camel]; ok {
			return v, true
		}
	}
	return nil, false
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.Title(parts[i])
	}
	return strings.Join(parts, "")
}

var geminiSamplingMappings = []paramMapping{
	{openai: "temperature", target: "temperature"},
	{openai: "top_p", target: "topP"},
	{openai: "top_k", target: "topK"},
	{openai: "max_tokens", target: "maxOutputTokens"},
	{openai: "max_completion_tokens", target: "maxOutputTokens"},
	{openai: "stop", target: "stopSequences"},
}

var anthropicSamplingMappings = []paramMapping{
	{openai: "temperature", target: "temperature"},
	{openai: "top_p", target: "top_p"},
	{openai: "top_k", target: "top_k"},
	{openai: "stop", target: "stop_sequences"},
}

var codexSamplingMappings = []paramMapping{
	{openai: "top_p", target: "top_p"},
	{openai: "presence_penalty", target: "presence_penalty"},
	{openai: "frequency_penalty", target: "frequency_penalty"},
	{openai: "stop", target: "stop"},
	{openai: "seed", target: "seed"},
}

// ── Utilities ──────────────────────────────────────────────────────────────

func shortID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func extractTextContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, p := range c {
			if pm, ok := p.(map[string]interface{}); ok {
				if t, ok := pm["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "")
	case nil:
		return ""
	default:
		return ""
	}
}

func stringVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func boolVal(m map[string]interface{}, key string) bool {
	v, _ := m[key].(bool)
	return v
}
