// Package translator converts between AI provider API formats.
// Core use case: accept Anthropic format from clients, translate to any provider format.
package translator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Format represents an API format type
type Format string

const (
	FormatAnthropic Format = "anthropic"
	FormatOpenAI    Format = "openai"
	FormatGemini    Format = "gemini"
)

// DetectFormat detects the API format from a request body
func DetectFormat(body map[string]interface{}) Format {
	// Anthropic has "messages" + top-level "system" field OR anthropic_version in headers
	// OpenAI has "messages" without top-level "system" (system is in messages array)
	if _, hasSystem := body["system"]; hasSystem {
		return FormatAnthropic
	}
	if _, hasMessages := body["messages"]; hasMessages {
		// Check for Anthropic-specific fields
		if _, hasMaxTokens := body["max_tokens"]; hasMaxTokens {
			// Could be either — check message structure
			if msgs, ok := body["messages"].([]interface{}); ok && len(msgs) > 0 {
				if msg, ok := msgs[0].(map[string]interface{}); ok {
					if role, ok := msg["role"].(string); ok && role == "user" {
						// Check if content has Anthropic-style content blocks
						if content, ok := msg["content"].([]interface{}); ok {
							if len(content) > 0 {
								if block, ok := content[0].(map[string]interface{}); ok {
									if blockType, ok := block["type"].(string); ok {
										if blockType == "tool_result" || blockType == "tool_use" {
											return FormatAnthropic
										}
									}
								}
							}
						}
					}
				}
			}
		}
		return FormatOpenAI
	}
	return FormatOpenAI // default
}

// DetectFormatFromHeaders detects format from HTTP headers
func DetectFormatFromHeaders(headerMap map[string]string) Format {
	if _, ok := headerMap["x-api-key"]; ok {
		return FormatAnthropic
	}
	if _, ok := headerMap["anthropic-version"]; ok {
		return FormatAnthropic
	}
	return FormatOpenAI
}

// AnthropicToOpenAIRequest converts an Anthropic messages request to OpenAI format
func AnthropicToOpenAIRequest(body map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// Copy simple fields
	if model, ok := body["model"].(string); ok {
		result["model"] = model
	}
	if maxTokens, ok := body["max_tokens"].(float64); ok {
		result["max_tokens"] = int(maxTokens)
	}
	if temp, ok := body["temperature"].(float64); ok {
		result["temperature"] = temp
	}
	if topP, ok := body["top_p"].(float64); ok {
		result["top_p"] = topP
	}
	if stream, ok := body["stream"].(bool); ok {
		result["stream"] = stream
		if stream {
			result["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}
	if stopSeq, ok := body["stop_sequences"].([]interface{}); ok {
		result["stop"] = stopSeq
	}

	// Convert messages
	var openaiMessages []interface{}

	// System prompt → system message
	if system, ok := body["system"]; ok {
		var systemText string
		switch s := system.(type) {
		case string:
			systemText = s
		case []interface{}:
			// Array of content blocks
			var parts []string
			for _, block := range s {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if blockType, ok := blockMap["type"].(string); ok && blockType == "text" {
						if text, ok := blockMap["text"].(string); ok {
							parts = append(parts, text)
						}
					}
				}
			}
			systemText = strings.Join(parts, "\n")
		}
		if systemText != "" {
			openaiMessages = append(openaiMessages, map[string]interface{}{
				"role":    "system",
				"content": systemText,
			})
		}
	}

	// Convert messages
	if messages, ok := body["messages"].([]interface{}); ok {
		for _, msg := range messages {
			msgMap, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			converted, err := convertAnthropicMessageToOpenAI(msgMap)
			if err != nil {
				return nil, fmt.Errorf("message conversion: %w", err)
			}
			if converted != nil {
				openaiMessages = append(openaiMessages, converted...)
			}
		}
	}

	result["messages"] = openaiMessages

	// thinking / extended thinking config (pass-through)
	if thinking, ok := body["thinking"].(map[string]interface{}); ok {
		result["thinking"] = thinking
	}

	// Convert tools
	if tools, ok := body["tools"].([]interface{}); ok {
		openaiTools := convertAnthropicToolsToOpenAI(tools)
		result["tools"] = openaiTools
	}

	// tool_choice
	if tc, ok := body["tool_choice"]; ok {
		result["tool_choice"] = convertAnthropicToolChoiceToOpenAI(tc)
	}

	// metadata (Anthropic-specific, map to user field if present)
	if meta, ok := body["metadata"].(map[string]interface{}); ok {
		if userID, ok := meta["user_id"].(string); ok && userID != "" {
			result["user"] = userID
		}
	}

	return result, nil
}

// convertAnthropicMessageToOpenAI converts a single Anthropic message to one or more OpenAI messages.
// Returns a slice because Anthropic user messages with tool_result blocks become multiple OpenAI messages.
func convertAnthropicMessageToOpenAI(msg map[string]interface{}) ([]interface{}, error) {
	role, _ := msg["role"].(string)
	content := msg["content"]

	switch role {
	case "user":
		return convertAnthropicUserMessage(msg, content)
	case "assistant":
		return convertAnthropicAssistantMessage(msg, content)
	default:
		// Pass through unknown roles
		return []interface{}{msg}, nil
	}
}

// convertAnthropicUserMessage handles Anthropic user messages.
// User messages can contain tool_result blocks (which become separate tool messages in OpenAI).
func convertAnthropicUserMessage(msg map[string]interface{}, content interface{}) ([]interface{}, error) {
	var result []interface{}

	switch c := content.(type) {
	case string:
		// Simple string content
		result = append(result, map[string]interface{}{
			"role":    "user",
			"content": c,
		})

	case []interface{}:
		// Array of content blocks — separate tool_results from text/image
		var toolResults []interface{}
		var textParts []interface{}

		for _, block := range c {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := blockMap["type"].(string)

			switch blockType {
			case "tool_result":
				// Collect tool results — emit after text blocks
				toolCallID, _ := blockMap["tool_use_id"].(string)
				isError, _ := blockMap["is_error"].(bool)

				var toolContent string
				switch tc := blockMap["content"].(type) {
				case string:
					toolContent = tc
				case []interface{}:
					var parts []string
					for _, item := range tc {
						if itemMap, ok := item.(map[string]interface{}); ok {
							if itemMap["type"] == "text" {
								if text, ok := itemMap["text"].(string); ok {
									parts = append(parts, text)
								}
							}
						}
					}
					toolContent = strings.Join(parts, "\n")
				}

				toolMsg := map[string]interface{}{
					"role":       "tool",
					"tool_call_id": toolCallID,
					"content":    toolContent,
				}
				if isError {
					toolMsg["content"] = "[ERROR] " + toolContent
				}
				toolResults = append(toolResults, toolMsg)

			case "text":
				if text, ok := blockMap["text"].(string); ok {
					textParts = append(textParts, map[string]interface{}{
						"type": "text",
						"text": text,
					})
				}

			case "image":
				source, ok := blockMap["source"].(map[string]interface{})
				if !ok {
					continue
				}
				mediaType, _ := source["media_type"].(string)
				data, _ := source["data"].(string)
				sourceType, _ := source["type"].(string)

				if sourceType == "base64" && data != "" {
					textParts = append(textParts, map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": fmt.Sprintf("data:%s;base64,%s", mediaType, data),
						},
					})
				}

			case "tool_use":
				// This shouldn't be in a user message, but handle it
				// tool_use in user messages is unusual but possible
			}
		}

		// Emit text parts first (as user message), then tool results
		if len(textParts) > 0 {
			if len(textParts) == 1 {
				if tp, ok := textParts[0].(map[string]interface{}); ok {
					if tp["type"] == "text" {
						result = append(result, map[string]interface{}{
							"role":    "user",
							"content": tp["text"],
						})
					}
				}
			} else {
				result = append(result, map[string]interface{}{
					"role":    "user",
					"content": textParts,
				})
			}
		}

		// Tool results come after text in OpenAI format
		result = append(result, toolResults...)

	default:
		// Unknown content type — pass through
		result = append(result, msg)
	}

	return result, nil
}

// convertAnthropicAssistantMessage handles Anthropic assistant messages.
// Extracts tool_use blocks into OpenAI tool_calls format.
func convertAnthropicAssistantMessage(msg map[string]interface{}, content interface{}) ([]interface{}, error) {
	result := map[string]interface{}{
		"role": "assistant",
	}

	switch c := content.(type) {
	case string:
		result["content"] = c

	case []interface{}:
		var textParts []string
		var toolCalls []interface{}

		for _, block := range c {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := blockMap["type"].(string)

			switch blockType {
			case "text":
				if text, ok := blockMap["text"].(string); ok {
					textParts = append(textParts, text)
				}

			case "tool_use":
				toolID, _ := blockMap["id"].(string)
				toolName, _ := blockMap["name"].(string)
				input, _ := blockMap["input"].(map[string]interface{})

				// Truncate tool name to 64 chars (provider limit)
				if len(toolName) > 64 {
					toolName = toolName[:64]
				}

				argsBytes, _ := json.Marshal(input)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   toolID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      toolName,
						"arguments": string(argsBytes),
					},
				})

			case "thinking":
				// Anthropic extended thinking — skip for OpenAI
				// (OpenAI doesn't have a direct equivalent)
			}
		}

		if len(textParts) > 0 {
			result["content"] = strings.Join(textParts, "\n")
		} else {
			result["content"] = nil
		}

		if len(toolCalls) > 0 {
			result["tool_calls"] = toolCalls
		}

	default:
		result["content"] = content
	}

	return []interface{}{result}, nil
}

// convertAnthropicToolsToOpenAI converts Anthropic tool definitions to OpenAI format
func convertAnthropicToolsToOpenAI(tools []interface{}) []interface{} {
	var result []interface{}

	for _, tool := range tools {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := toolMap["name"].(string)
		desc, _ := toolMap["description"].(string)
		inputSchema, _ := toolMap["input_schema"].(map[string]interface{})

		// Truncate tool name to 64 chars (provider limit)
		if len(name) > 64 {
			name = name[:64]
		}

		// Anthropic input_schema → OpenAI parameters (same JSON Schema structure)
		parameters := make(map[string]interface{})
		for k, v := range inputSchema {
			parameters[k] = v
		}

		result = append(result, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": desc,
				"parameters":  parameters,
			},
		})
	}

	return result
}

// convertAnthropicToolChoiceToOpenAI converts Anthropic tool_choice to OpenAI format
func convertAnthropicToolChoiceToOpenAI(tc interface{}) interface{} {
	tcMap, ok := tc.(map[string]interface{})
	if !ok {
		return tc
	}

	tcType, _ := tcMap["type"].(string)
	switch tcType {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		if name, ok := tcMap["name"].(string); ok {
			return map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": name,
				},
			}
		}
		return "auto"
	default:
		return "auto"
	}
}

// OpenAIToAnthropicResponse converts an OpenAI response to Anthropic format
func OpenAIToAnthropicResponse(openaiResp map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{
		"type": "message",
	}

	// ID — prefix with "msg_" if not already
	if id, ok := openaiResp["id"].(string); ok {
		if !strings.HasPrefix(id, "msg_") {
			result["id"] = "msg_" + id
		} else {
			result["id"] = id
		}
	} else {
		result["id"] = "msg_" + genID()
	}

	result["role"] = "assistant"

	// Model
	if model, ok := openaiResp["model"].(string); ok {
		result["model"] = model
	}

	// Content
	if choices, ok := openaiResp["choices"].([]interface{}); ok && len(choices) > 0 {
		choice, ok := choices[0].(map[string]interface{})
		if ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				var contentBlocks []interface{}

				// Text content
				if text, ok := message["content"].(string); ok && text != "" {
					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type": "text",
						"text": text,
					})
				}

				// Tool calls
				if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						tcMap, ok := tc.(map[string]interface{})
						if !ok {
							continue
						}
						fn, ok := tcMap["function"].(map[string]interface{})
						if !ok {
							continue
						}

						toolID, _ := tcMap["id"].(string)
						toolName, _ := fn["name"].(string)
						argsStr, _ := fn["arguments"].(string)

						var input map[string]interface{}
						json.Unmarshal([]byte(argsStr), &input)
						if input == nil {
							input = map[string]interface{}{}
						}

						contentBlocks = append(contentBlocks, map[string]interface{}{
							"type":  "tool_use",
							"id":    toolID,
							"name":  toolName,
							"input": input,
						})
					}
				}

					// Reasoning / thinking summary (OpenAI-style reasoning content)
					if reasoning, ok := message["reasoning"].(string); ok && reasoning != "" {
						contentBlocks = append(contentBlocks, map[string]interface{}{
							"type": "thinking",
							"thinking": reasoning,
						})
					}

					if len(contentBlocks) == 0 {
						contentBlocks = []interface{}{}
					}
					result["content"] = contentBlocks

				// Stop reason
				finishReason, _ := choice["finish_reason"].(string)
				result["stop_reason"] = mapFinishReason(finishReason)
			}
		}
	}

	// Usage
	if usage, ok := openaiResp["usage"].(map[string]interface{}); ok {
		anthropicUsage := map[string]interface{}{}
		if pt, ok := usage["prompt_tokens"].(float64); ok {
			anthropicUsage["input_tokens"] = int(pt)
		}
		if ct, ok := usage["completion_tokens"].(float64); ok {
			anthropicUsage["output_tokens"] = int(ct)
		}
		result["usage"] = anthropicUsage
	}

	return result
}

// mapFinishReason maps OpenAI finish_reason to Anthropic stop_reason
func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// genID generates a simple unique ID for messages
func genID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "msg_" + hex.EncodeToString(b)
}

// AnthropicToGeminiRequest converts an Anthropic messages request to Gemini format
func AnthropicToGeminiRequest(body map[string]interface{}) (map[string]interface{}, error) {
	// First convert to OpenAI, then use existing Anigravity/Gemini conversion
	// This is a two-step approach: Anthropic → OpenAI → Gemini
	openaiBody, err := AnthropicToOpenAIRequest(body)
	if err != nil {
		return nil, err
	}
	return openaiBody, nil // The actual Gemini conversion happens in anigravity.go
}
