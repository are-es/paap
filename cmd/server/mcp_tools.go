package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

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
			Name:        "web_search",
			Description: "Search the web using Firecrawl. Returns titles, URLs, and snippets. USE WHEN: user asks to search, look up, find information, research a topic, check current events, verify facts, or needs any information not in your training data.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Search query. Be specific for better results. E.g. 'Go 1.24 release notes' not 'Go'"},
					"limit": map[string]interface{}{"type": "integer", "description": "Number of results (1-10, default 5)", "default": 5},
				},
				"required": []string{"query"},
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
	case "web_search":
		return mcpHandleWebSearch(args)
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

// ── web_search ────────────────────────────────────────────────

// SearchResult represents a single search result
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// searchFirecrawl searches via Firecrawl API
func searchFirecrawl(query string, limit int, apiKey string) ([]SearchResult, error) {
	body, _ := json.Marshal(map[string]interface{}{"query": query, "limit": limit})
	req, err := http.NewRequest("POST", "https://api.firecrawl.dev/v1/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("firecrawl %d: %s", resp.StatusCode, truncateStr(string(respBody), 100))
	}

	var result struct {
		Success bool `json:"success"`
		Data    []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if !result.Success || len(result.Data) == 0 {
		return nil, fmt.Errorf("no results")
	}

	var results []SearchResult
	for _, r := range result.Data {
		desc := strings.ReplaceAll(r.Description, "\n", " ")
		if len(desc) > 300 {
			desc = desc[:300] + "..."
		}
		results = append(results, SearchResult{r.Title, r.URL, desc})
	}
	return results, nil
}

// searchBrave searches via Brave Search API
func searchBrave(query string, limit int, apiKey string) ([]SearchResult, error) {
	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d", url.QueryEscape(query), limit)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("brave %d: %s", resp.StatusCode, truncateStr(string(respBody), 100))
	}

	var result struct {
		Web struct {
			Results []struct {
				URL         string `json:"url"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if len(result.Web.Results) == 0 {
		return nil, fmt.Errorf("no results")
	}

	var results []SearchResult
	for _, r := range result.Web.Results {
		results = append(results, SearchResult{r.Title, r.URL, r.Description})
	}
	return results, nil
}

// searchDuckDuckGo searches via DuckDuckGo (free, no API key)
func searchDuckDuckGo(query string, limit int) ([]SearchResult, error) {
	// Use ddgs Python library via subprocess
	cmd := fmt.Sprintf("from ddgs import DDGS\nimport json\nresults = DDGS().text('%s', max_results=%d)\nprint(json.dumps(results))", query, limit)

	out, err := exec.Command("python3", "-c", cmd).Output()
	if err != nil {
		return nil, fmt.Errorf("ddgs failed: %v", err)
	}

	var ddgResults []struct {
		Title string `json:"title"`
		Href  string `json:"href"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(out, &ddgResults); err != nil {
		return nil, fmt.Errorf("ddgs parse error: %v", err)
	}
	if len(ddgResults) == 0 {
		return nil, fmt.Errorf("no results")
	}

	var results []SearchResult
	for _, r := range ddgResults {
		results = append(results, SearchResult{r.Title, r.Href, r.Body})
	}
	return results, nil
}

// searchSearXNG searches via self-hosted SearXNG instance
func searchSearXNG(query string, limit int, baseURL string) ([]SearchResult, error) {
	reqURL := fmt.Sprintf("%s/search?q=%s&format=json&pageno=1", strings.TrimRight(baseURL, "/"), url.QueryEscape(query))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("searxng %d: %s", resp.StatusCode, truncateStr(string(respBody), 100))
	}

	var result struct {
		Results []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("no results")
	}

	var results []SearchResult
	for i, r := range result.Results {
		if i >= limit {
			break
		}
		results = append(results, SearchResult{r.Title, r.URL, r.Content})
	}
	return results, nil
}

// formatSearchResults formats search results for display
func formatSearchResults(results []SearchResult) string {
	var sb strings.Builder
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func mcpHandleWebSearch(args json.RawMessage) interface{} {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return mcpToolError("invalid arguments: " + err.Error())
	}
	if params.Query == "" {
		return mcpToolError("query is required")
	}
	if params.Limit <= 0 || params.Limit > 10 {
		params.Limit = 5
	}

	// Get search config from DB
	mode := getSettingStrCached("search_provider_mode", "auto")
	override := getSettingStrCached("search_provider", "")

	// Build provider chain
	var providers []string
	if mode == "manual" && override != "" {
		providers = []string{override}
	} else {
		// Auto-fallback chain: try configured providers first, DDG as fallback
		providers = []string{"firecrawl", "brave", "searxng", "duckduckgo"}
	}

	var lastErr error
	for _, provider := range providers {
		var results []SearchResult
		var err error

		switch provider {
		case "firecrawl":
			key := getSettingStrCached("search_firecrawl_key", "")
			if key == "" {
				key = os.Getenv("FIRECRAWL_API_KEY")
			}
			if key == "" {
				continue // skip, no key
			}
			results, err = searchFirecrawl(params.Query, params.Limit, key)
		case "brave":
			key := getSettingStrCached("search_brave_key", "")
			if key == "" {
				continue
			}
			results, err = searchBrave(params.Query, params.Limit, key)
		case "searxng":
			searxURL := getSettingStrCached("search_searxng_url", "")
			if searxURL == "" {
				continue
			}
			results, err = searchSearXNG(params.Query, params.Limit, searxURL)
		case "duckduckgo":
			results, err = searchDuckDuckGo(params.Query, params.Limit)
		default:
			err = fmt.Errorf("unknown provider: %s", provider)
		}

		if err == nil && len(results) > 0 {
			log.Printf("[PAAP] [MCP] web_search provider=%s query=%q results=%d", provider, params.Query, len(results))
			return mcpToolResult(formatSearchResults(results))
		}

		if err != nil {
			log.Printf("[PAAP] [MCP] web_search provider=%s failed: %v", provider, err)
			lastErr = err
		}
	}

	if lastErr != nil {
		return mcpToolError("all search providers failed: " + lastErr.Error())
	}
	return mcpToolResult("No results found for: " + params.Query)
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
		"model":      visionModel,
		"messages":   messages,
		"max_tokens": 1024,
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", internalAPIBaseURL()+"/v1/chat/completions", bytes.NewReader(bodyJSON))
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
