package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/dolvin/paap/internal/db"
)

// ── MCP Tool Helpers ──────────────────────────────────────────

func mcpToolResult(text string) interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}
}

func mcpToolError(msg string) interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": msg},
		},
		"isError": true,
	}
}

// ── Tool Registry ─────────────────────────────────────────────

func mcpToolsList() []mcpToolDef {
	if getSettingStrCached("mcp_enabled", "true") != "true" {
		return nil
	}
	return []mcpToolDef{
		{
			Name:        "generate_image",
			Description: "Generate an image from a text prompt. Transforms simple descriptions into rich, specific prompts with style direction, composition, and color palette. Returns image URL. USE WHEN: user asks to create/draw/make/design any visual — illustrations, logos, posters, photos, art, diagrams, UI mockups, memes, characters, scenes.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{"type": "string", "description": "Text description of the image. Be specific about subject, style, composition, colors, lighting, and mood. Vague prompts produce generic results. Include art direction in the prompt itself (e.g. 'ink wash painting style', 'analog film grain', 'Risograph print')."},
					"size":   map[string]interface{}{"type": "string", "description": "Image dimensions: '1024x1024' (square), '1360x768' (landscape), '768x1360' (portrait), '896x1184' (tall), '1184x896' (wide)", "default": "1024x1024"},
				},
				"required": []string{"prompt"},
			},
		},
		{
			Name:        "text_to_speech",
			Description: "Convert text to natural-sounding speech audio. Returns audio file URL. USE WHEN: user asks for voice output, audio narration, TTS, 'say this', 'read aloud', or wants to hear pronunciation.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":  map[string]interface{}{"type": "string", "description": "Text to speak. Include punctuation for natural pacing. SSML tags supported for advanced control."},
					"voice": map[string]interface{}{"type": "string", "description": "Voice name or ID. Varies by provider. Leave empty for default.", "default": ""},
					"model": map[string]interface{}{"type": "string", "description": "TTS model override. Leave empty for provider default.", "default": ""},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "analyze_image",
			Description: "Analyze and describe an image using a vision model. Returns detailed textual description. USE WHEN: user shares an image URL and asks 'what is this?', 'describe this', needs OCR, wants to understand content, or asks questions about a visual.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"image_url": map[string]interface{}{"type": "string", "description": "URL of the image to analyze (http/https). Must be publicly accessible."},
					"prompt":    map[string]interface{}{"type": "string", "description": "Specific question or instruction about the image. E.g. 'What text is in this image?', 'Describe the mood', 'Count the objects'.", "default": "Describe this image in detail."},
				},
				"required": []string{"image_url"},
			},
		},
	}
}

func mcpToolCall(name string, args json.RawMessage) interface{} {
	switch name {
	case "generate_image":
		return mcpHandleGenerateImage(args)
	case "text_to_speech":
		return mcpHandleTextToSpeech(args)
	case "analyze_image":
		return mcpHandleAnalyzeImage(args)
	default:
		return mcpToolError(fmt.Sprintf("unknown tool: %s", name))
	}
}

// ── generate_image ────────────────────────────────────────────

func mcpHandleGenerateImage(args json.RawMessage) interface{} {
	var params struct {
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return mcpToolError("invalid arguments: " + err.Error())
	}
	if params.Prompt == "" {
		return mcpToolError("prompt is required")
	}

 imageURL, err := handleGenerateImage(params.Prompt, params.Size, "")
	if err != nil {
		return mcpToolError(err.Error())
	}

	return mcpToolResult(imageURL)
}

// ── text_to_speech ────────────────────────────────────────────

func mcpHandleTextToSpeech(args json.RawMessage) interface{} {
	var params struct {
		Text  string `json:"text"`
		Voice string `json:"voice"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return mcpToolError("invalid arguments: " + err.Error())
	}
	if params.Text == "" {
		return mcpToolError("text is required")
	}

	audioData, err := handleTextToSpeech(params.Text, params.Voice, params.Model)
	if err != nil {
		return mcpToolError(err.Error())
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{{
			"type":     "text",
			"text":     audioData,
			"mimeType": "audio/mpeg",
		}},
	}
}

// ── analyze_image ─────────────────────────────────────────────

func mcpHandleAnalyzeImage(args json.RawMessage) interface{} {
	var params struct {
		ImageURL string `json:"image_url"`
		Prompt   string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return mcpToolError("invalid arguments: " + err.Error())
	}
	if params.ImageURL == "" {
		return mcpToolError("image_url is required")
	}
	if params.Prompt == "" {
		params.Prompt = "Describe this image in detail."
	}

	visionModel := getSettingStrCached("vision_model", "")
	if visionModel == "" {
		var routeModel string
		dbErr := db.DB.QueryRow("SELECT route_model FROM tools WHERE tool_type='vision' AND enabled=1 LIMIT 1").Scan(&routeModel)
		if dbErr == nil {
			visionModel = routeModel
		}
	}

	if visionModel == "" {
		return mcpToolError("no vision model configured — enable vision tool in Tools settings")
	}

	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{"type": "text", "text": params.Prompt},
				{"type": "image_url", "image_url": map[string]string{"url": params.ImageURL}},
			},
		},
	}

	body := map[string]interface{}{
		"model":    visionModel,
		"messages": messages,
		"max_tokens": 1024,
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "http://127.0.0.1:9090/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return mcpToolError("failed to create request: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120_000_000_000}
	resp, err := client.Do(req)
	if err != nil {
		return mcpToolError("vision request failed: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return mcpToolError(fmt.Sprintf("vision provider returned %d: %s", resp.StatusCode, string(errBody)))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return mcpToolError("failed to parse vision response: " + err.Error())
	}

	if len(result.Choices) == 0 {
		return mcpToolError("vision provider returned no choices")
	}

	description := result.Choices[0].Message.Content
	log.Printf("[PAAP] [MCP] [Vision] Analyzed image (%s)", params.ImageURL[:min(50, len(params.ImageURL))])
	return mcpToolResult(description)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
