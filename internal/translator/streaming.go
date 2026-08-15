package translator

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// StreamTranslator converts OpenAI SSE streaming chunks to Anthropic SSE format.
// It maintains state across chunks to emit proper Anthropic event sequences.
type StreamTranslator struct {
	writer     io.Writer
	flusher    func()
	model      string
	inputTokens int

	started        bool
	messageStartSent bool
	blockStarted   bool
	blockIndex     int
	hasToolCalls   bool
	toolCallIndex  int
	currentToolID  string
	currentToolName string
	currentToolArgs strings.Builder
}

// NewStreamTranslator creates a new streaming translator
func NewStreamTranslator(w io.Writer, flush func(), model string) *StreamTranslator {
	return &StreamTranslator{
		writer:  w,
		flusher: flush,
		model:   model,
	}
}

// SetInputTokens sets the input token count (from the first chunk or separate info)
func (st *StreamTranslator) SetInputTokens(n int) {
	st.inputTokens = n
}

// EstimateInputTokens estimates input tokens from an OpenAI request body.
// Uses ~4 chars per token as a rough heuristic (works for English + code).
// Better than showing 0 to the client.
func EstimateInputTokens(body map[string]interface{}) int {
	var totalChars int

	if system, ok := body["system"].(string); ok {
		totalChars += len(system)
	}

	if messages, ok := body["messages"].([]interface{}); ok {
		for _, msg := range messages {
			msgMap, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			switch content := msgMap["content"].(type) {
			case string:
				totalChars += len(content)
			case []interface{}:
				for _, block := range content {
					if blockMap, ok := block.(map[string]interface{}); ok {
						if text, ok := blockMap["text"].(string); ok {
							totalChars += len(text)
						}
						if tc, ok := blockMap["content"].(string); ok {
							totalChars += len(tc)
						}
					}
				}
			}
		}
	}

	// ~4 chars per token is a reasonable estimate
	tokens := totalChars / 4
	if tokens < 100 {
		tokens = 100 // minimum floor
	}
	return tokens
}

// ProcessChunk processes a single OpenAI SSE chunk and emits Anthropic events.
// Returns output tokens if available.
func (st *StreamTranslator) ProcessChunk(chunk map[string]interface{}) (outputTokens int) {
	// Extract choice
	choices, ok := chunk["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		// Might be a usage-only chunk
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			if ct, ok := usage["completion_tokens"].(float64); ok {
				outputTokens = int(ct)
			}
			if pt, ok := usage["prompt_tokens"].(float64); ok && st.inputTokens == 0 {
				st.inputTokens = int(pt)
			}
		}
		return
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return
	}

	delta, _ := choice["delta"].(map[string]interface{})
	finishReason, _ := choice["finish_reason"].(string)

	// Emit message_start if not done yet
	if !st.started {
		st.emitMessageStart()
		st.started = true
	}

	// Handle delta content
	if delta != nil {
		// Text content
		if content, ok := delta["content"].(string); ok && content != "" {
			if !st.blockStarted {
				st.emitContentBlockStart("text")
				st.blockStarted = true
			}
			st.emitTextDelta(content)
		}

		// Tool calls
		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
			for _, tc := range toolCalls {
				tcMap, ok := tc.(map[string]interface{})
				if !ok {
					continue
				}

				tcIndex, _ := tcMap["index"].(float64)
				fn, _ := tcMap["function"].(map[string]interface{})

				// New tool call
				if id, ok := tcMap["id"].(string); ok && id != "" {
					// Close previous tool block if any
					if st.hasToolCalls && st.currentToolID != "" {
						st.emitToolInputDelta("") // flush args
						st.emitContentBlockStop()
					}

					st.currentToolID = id
					st.currentToolName = ""
					st.currentToolArgs.Reset()
					if fn != nil {
						if name, ok := fn["name"].(string); ok {
							st.currentToolName = name
						}
					}
					st.hasToolCalls = true
					st.blockIndex = int(tcIndex)
					st.emitToolUseStart(st.blockIndex, st.currentToolID, st.currentToolName)
				}

				// Tool arguments delta
				if fn != nil {
					if args, ok := fn["arguments"].(string); ok && args != "" {
						st.currentToolArgs.WriteString(args)
						st.emitToolInputDelta(args)
					}
				}
			}
		}
	}

	// Extract usage from chunk
	if usage, ok := chunk["usage"].(map[string]interface{}); ok {
		if ct, ok := usage["completion_tokens"].(float64); ok {
			outputTokens = int(ct)
		}
		if pt, ok := usage["prompt_tokens"].(float64); ok && st.inputTokens == 0 {
			st.inputTokens = int(pt)
		}
	}

	// Handle finish
	if finishReason != "" {
		// Close any open content block
		if st.blockStarted {
			st.emitContentBlockStop()
			st.blockStarted = false
		}
		if st.hasToolCalls && st.currentToolID != "" {
			// Tool block already closed above if new tool came, but close if last
			// Actually we need to close the last tool block
			st.emitContentBlockStop()
			st.currentToolID = ""
		}

		stopReason := mapFinishReason(finishReason)
		st.emitMessageDelta(stopReason, outputTokens)
		st.emitMessageStop()
	}

	return
}

// ProcessReader reads OpenAI SSE stream and converts to Anthropic SSE format.
// Returns (tokensIn, tokensOut, fullContent).
func (st *StreamTranslator) ProcessReader(r io.Reader) (int, int, string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var tokensOut int
	var fullContent strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			// If we haven't emitted stop yet (no finish_reason chunk), do it now
			if st.started && (st.blockStarted || st.hasToolCalls) {
				st.emitContentBlockStop()
				st.emitMessageDelta("end_turn", 0)
				st.emitMessageStop()
			}
			break
		}

		var chunk map[string]interface{}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}

		// Capture content from delta for logging
		if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if content, ok := delta["content"].(string); ok {
						fullContent.WriteString(content)
					}
				}
			}
		}

		out := st.ProcessChunk(chunk)
		if out > 0 {
			tokensOut = out
		}
		if pt, ok := chunk["usage"].(map[string]interface{}); ok {
			if prompt, ok := pt["prompt_tokens"].(float64); ok && prompt > 0 {
				st.inputTokens = int(prompt)
			}
		}
	}

	return st.inputTokens, tokensOut, fullContent.String()
}

// ── Anthropic SSE Event Emitters ─────────────────────────────

func (st *StreamTranslator) writeEvent(event string, data interface{}) {
	dataBytes, _ := json.Marshal(data)
	st.writer.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(dataBytes))))
	if st.flusher != nil {
		st.flusher()
	}
}

func (st *StreamTranslator) emitMessageStart() {
	st.writeEvent("message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            genID(),
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{},
			"model":         st.model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]interface{}{
				"input_tokens":  st.inputTokens,
				"output_tokens": 0,
			},
		},
	})
}

func (st *StreamTranslator) emitContentBlockStart(blockType string) {
	st.writeEvent("content_block_start", map[string]interface{}{
		"type":       "content_block_start",
		"index":      0,
		"content_block": map[string]interface{}{
			"type": blockType,
			"text": "",
		},
	})
}

func (st *StreamTranslator) emitTextDelta(text string) {
	st.writeEvent("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": text,
		},
	})
}

func (st *StreamTranslator) emitToolUseStart(index int, id, name string) {
	st.writeEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]interface{}{
			"type":  "tool_use",
			"id":    id,
			"name":  name,
			"input": map[string]interface{}{},
		},
	})
}

func (st *StreamTranslator) emitToolInputDelta(args string) {
	if args == "" {
		return
	}
	st.writeEvent("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": st.blockIndex,
		"delta": map[string]interface{}{
			"type":        "input_json_delta",
			"partial_json": args,
		},
	})
}

func (st *StreamTranslator) emitContentBlockStop() {
	idx := 0
	if st.hasToolCalls {
		idx = st.blockIndex
	}
	st.writeEvent("content_block_stop", map[string]interface{}{
		"type":  "content_block_stop",
		"index": idx,
	})
}

func (st *StreamTranslator) emitMessageDelta(stopReason string, outputTokens int) {
	st.writeEvent("message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"output_tokens": outputTokens,
		},
	})
}

func (st *StreamTranslator) emitMessageStop() {
	st.writeEvent("message_stop", map[string]interface{}{
		"type": "message_stop",
	})
}
