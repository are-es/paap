package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dolvin/paap/internal/db"
)

// ── Provider Type Detection ─────────────────────────────────

// detectProviderType returns the type of provider based on base_url and known patterns
func detectProviderType(baseURL string) string {
	lower := strings.ToLower(baseURL)
	switch {
	case strings.Contains(lower, "stepfun"):
		return "stepfun"
	case strings.Contains(lower, "xiaomimimo") || strings.Contains(lower, "mimo"):
		return "mimo"
	case strings.Contains(lower, "generativelanguage.googleapis.com"):
		return "google"
	case strings.Contains(lower, "openai.com"):
		return "openai"
	case strings.Contains(lower, "openrouter"):
		return "openrouter"
	default:
		return "openai" // default: assume OpenAI-compatible
	}
}

// ── Image Generation Adapters ───────────────────────────────

// handleGenerateImage dispatches to the correct adapter based on provider type
func handleGenerateImage(prompt, size, model string) (string, error) {
	providerID := getSettingStrCached("mcp_image_provider", "")
	if providerID == "" {
		return "", fmt.Errorf("no image generation provider configured — set mcp_image_provider in settings")
	}

	var baseURL string
	err := db.DB.QueryRow("SELECT base_url FROM providers WHERE id=?", providerID).Scan(&baseURL)
	if err != nil {
		return "", fmt.Errorf("image provider %q not found: %w", providerID, err)
	}

	apiKey, err := getProviderKey(providerID)
	if err != nil {
		return "", err
	}

	if size == "" {
		size = "1024x1024"
	}
	if model == "" {
		model = getSettingStrCached("mcp_image_model", "")
	}

	providerType := detectProviderType(baseURL)
	log.Printf("[PAAP] [MCP] [ImageGen] Provider: %s, Type: %s, Model: %s", providerID, providerType, model)

	switch providerType {
	case "stepfun":
		return imageGenStepFun(baseURL, apiKey, prompt, size, model)
	case "google":
		return imageGenGoogle(baseURL, apiKey, prompt, size, model)
	default:
		return imageGenOpenAI(baseURL, apiKey, prompt, size, model)
	}
}

// imageGenOpenAI — standard /v1/images/generations
func imageGenOpenAI(baseURL, apiKey, prompt, size, model string) (string, error) {
	body := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"size":   size,
		"n":      1,
	}
	return postImageGen(baseURL+"/v1/images/generations", apiKey, body)
}

// imageGenStepFun — uses step_plan path
func imageGenStepFun(baseURL, apiKey, prompt, size, model string) (string, error) {
	// StepFun uses base_url directly (already includes /step_plan/v1)
	body := map[string]interface{}{
		"model":           model,
		"prompt":          prompt,
		"size":            size,
		"response_format": "url",
		"cfg_scale":       1.0,
		"steps":           8,
	}
	url := baseURL + "/images/generations"
	return postImageGen(url, apiKey, body)
}

// imageGenGoogle — Google AI Studio native format
func imageGenGoogle(baseURL, apiKey, prompt, size, model string) (string, error) {
	// Google uses /v1beta/images:generate with API key in URL
	url := strings.Replace(baseURL, "/v1beta", "", 1) + "/v1beta/images:generate?key=" + apiKey

	// Parse size to aspect ratio
	aspectRatio := "1:1"
	if size == "768x1360" || size == "512x768" {
		aspectRatio = "9:16"
	} else if size == "1360x768" || size == "768x512" {
		aspectRatio = "16:9"
	}

	body := map[string]interface{}{
		"prompt": map[string]string{"text": prompt},
		"config": map[string]interface{}{
			"numberOfImages": 1,
			"aspectRatio":    aspectRatio,
		},
	}
	if model != "" {
		body["model"] = model
	}

	bodyJSON, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120_000_000_000}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("google image gen failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("google returned %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Images []struct {
			ImageBytes string `json:"imageBytes"`
			MimeType   string `json:"mimeType"`
		} `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse google response: %w", err)
	}

	if len(result.Images) == 0 {
		return "", fmt.Errorf("google returned no images")
	}

	mimeType := result.Images[0].MimeType
	if mimeType == "" {
		mimeType = "image/png"
	}
	log.Printf("[PAAP] [MCP] [ImageGen] Google generated image (%s)", mimeType)
	return "data:" + mimeType + ";base64," + result.Images[0].ImageBytes, nil
}

// postImageGen — common OpenAI-compatible image gen request
func postImageGen(url, apiKey string, body map[string]interface{}) (string, error) {
	bodyJSON, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120_000_000_000}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("image gen failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("provider returned no data")
	}

	d := result.Data[0]
	if d.URL != "" {
		log.Printf("[PAAP] [MCP] [ImageGen] Generated image URL")
		return d.URL, nil
	}
	if d.B64JSON != "" {
		log.Printf("[PAAP] [MCP] [ImageGen] Generated image base64")
		return "data:image/png;base64," + d.B64JSON, nil
	}

	return "", fmt.Errorf("provider returned no URL or data")
}

// ── TTS Adapters ────────────────────────────────────────────

// handleTextToSpeech dispatches to the correct adapter based on provider type
func handleTextToSpeech(text, voice, model string) (string, error) {
	providerID := getSettingStrCached("mcp_tts_provider", "")
	if providerID == "" {
		return "", fmt.Errorf("no TTS provider configured — set mcp_tts_provider in settings")
	}

	var baseURL string
	err := db.DB.QueryRow("SELECT base_url FROM providers WHERE id=?", providerID).Scan(&baseURL)
	if err != nil {
		return "", fmt.Errorf("TTS provider %q not found: %w", providerID, err)
	}

	apiKey, err := getProviderKey(providerID)
	if err != nil {
		return "", err
	}

	if voice == "" {
		voice = getSettingStr("mcp_tts_voice", "")
	}
	if model == "" {
		model = getSettingStr("mcp_tts_model", "")
	}

	providerType := detectProviderType(baseURL)
	log.Printf("[PAAP] [MCP] [TTS] Provider: %s, Type: %s, Model: %s", providerID, providerType, model)

	switch providerType {
	case "mimo":
		return ttsMiMo(baseURL, apiKey, text, voice, model)
	case "stepfun":
		return ttsStepFun(baseURL, apiKey, text, voice, model)
	default:
		return ttsOpenAI(baseURL, apiKey, text, voice, model)
	}
}

// ttsOpenAI — standard /v1/audio/speech
func ttsOpenAI(baseURL, apiKey, text, voice, model string) (string, error) {
	if voice == "" {
		voice = "alloy"
	}
	if model == "" {
		model = "tts-1"
	}

	body := map[string]interface{}{
		"model": model,
		"input": text,
		"voice": voice,
	}
	return postTTS(baseURL+"/v1/audio/speech", apiKey, body, false)
}

// ttsStepFun — uses /v1/audio/speech (not step_plan path)
func ttsStepFun(baseURL, apiKey, text, voice, model string) (string, error) {
	if voice == "" {
		voice = "lively-girl"
	}
	if model == "" {
		model = "step-tts-2"
	}

	body := map[string]interface{}{
		"model": model,
		"input": text,
		"voice": voice,
	}
	// StepFun TTS is at /v1/ not /step_plan/v1/
	url := strings.Replace(baseURL, "step_plan/v1", "v1", 1) + "/audio/speech"
	return postTTS(url, apiKey, body, false)
}

// ttsMiMo — uses /v1/chat/completions with audio parameter
func ttsMiMo(baseURL, apiKey, text, voice, model string) (string, error) {
	if voice == "" {
		voice = "Chloe"
	}
	if model == "" {
		model = "mimo-v2.5-tts"
	}

	// MiMo TTS format: chat completions with audio param
	// user message = style instruction, assistant message = text to speak
	messages := []map[string]string{
		{"role": "user", "content": "Speak clearly and naturally."},
		{"role": "assistant", "content": text},
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"audio": map[string]string{
			"format": "wav",
			"voice":  voice,
		},
	}

	url := baseURL + "/chat/completions"
	bodyJSON, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120_000_000_000}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mimo tts failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("mimo returned %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse mimo response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("mimo returned no choices")
	}

	audioData := result.Choices[0].Message.Audio.Data
	if audioData == "" {
		return "", fmt.Errorf("mimo returned no audio data")
	}

	log.Printf("[PAAP] [MCP] [TTS] MiMo generated audio")
	return "data:audio/wav;base64," + audioData, nil
}

// postTTS — common OpenAI-compatible TTS request
func postTTS(url, apiKey string, body map[string]interface{}, returnBase64 bool) (string, error) {
	bodyJSON, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120_000_000_000}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tts failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(errBody))
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read audio: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(audioBytes)
	log.Printf("[PAAP] [MCP] [TTS] Generated audio (%d bytes)", len(audioBytes))
	return "data:audio/mpeg;base64," + b64, nil
}

// ── Helper ──────────────────────────────────────────────────

func getProviderKey(providerID string) (string, error) {
	var keyEnc string
	err := db.DB.QueryRow("SELECT key_encrypted FROM api_keys WHERE provider_id=? AND is_active=1 LIMIT 1", providerID).Scan(&keyEnc)
	if err != nil {
		return "", fmt.Errorf("no active API key for provider %q: %w", providerID, err)
	}
	return keyEnc, nil
}
