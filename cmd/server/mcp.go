package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// ── MCP Protocol Types ────────────────────────────────────────

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type mcpToolCallResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// ── MCP Handler ───────────────────────────────────────────────

func mcpMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if getSettingStrCached("mcp_enabled", "true") != "true" {
		mcpWriteError(w, nil, -32600, "MCP server is disabled")
		return
	}

	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		mcpWriteError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	if req.JSONRPC != "2.0" {
		mcpWriteError(w, req.ID, -32600, "invalid JSON-RPC version")
		return
	}

	switch req.Method {
	case "initialize":
		mcpHandleInitialize(w, req)
	case "tools/list":
		mcpHandleToolsList(w, req)
	case "tools/call":
		mcpHandleToolsCall(w, req)
	default:
		mcpWriteError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func mcpStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": getSettingStrCached("mcp_enabled", "true") == "true",
		"version": "2024-11-05",
		"tools":   len(mcpGetRegisteredTools()),
	})
}

// ── MCP Methods ───────────────────────────────────────────────

func mcpHandleInitialize(w http.ResponseWriter, req mcpRequest) {
	log.Printf("[PAAP] [MCP] Client initialized")
	mcpWriteResult(w, req.ID, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "PAAP",
			"version": "1.0.0",
		},
		"instructions": `You are connected to PAAP — a multimedia proxy gateway.

Available tools:
- generate_image: Create images from text descriptions. USE THIS when users ask to create, draw, make, or design any visual content (illustrations, logos, posters, photos, art, diagrams, UI mockups).
- text_to_speech: Convert text to spoken audio. USE THIS when users ask for voice, audio, TTS, or "say this out loud".
- analyze_image: Describe and analyze images. USE THIS when users share an image URL and ask "what is this?", "describe this", or need image understanding.

RULES FOR IMAGE GENERATION — ANTI-AI-SLOP:
When calling generate_image, transform the user's request into a prompt that produces DISTINCTIVE, non-generic output. Apply these principles:

1. SPECIFIC > GENERIC: "a cat" → "a battle-scarred ginger tomcat sitting on a rain-soaked Tokyo rooftop at 3am, neon signs reflecting in puddles"
2. STYLE ANCHORING: Always specify an explicit art direction — editorial illustration, brutalist poster, film noir still, Y2K aesthetic, Japanese woodblock print, Soviet constructivism, analog photography, Risograph print, etc.
3. COMPOSITION: Mention camera angle, framing, lighting — "shot from below", "bird's eye view", "dramatic side lighting", "flat lay", "Dutch angle"
4. COLOR PALETTE: Name specific colors or moods — "muted earth tones with one electric cyan accent", "monochrome with selective red", "pastel vaporwave palette"
5. TEXTURE & MEDIUM: "visible brushstrokes", "grainy film stock", "screen-printed halftone dots", "pencil sketch on kraft paper", "3D rendered with clay material"
6. NO CLICHES: Avoid "beautiful", "stunning", "detailed", "professional", "high quality", "masterpiece". These add nothing.
7. CONTEXT: If user says "make it cool" → interpret as specific aesthetic (cyberpunk, brutalist, retro-futurist). Never default to generic "professional" look.
8. NEGATIVE SPACE: If the image should be minimal, say so. Not everything needs to be "highly detailed".

EXAMPLE TRANSFORMATIONS:
User: "make me a logo for a coffee shop"
→ "Minimalist logo mark for specialty coffee roaster, single continuous line drawing of a coffee cherry branch, warm terracotta on cream background, Swiss design influence, clean vector style, no text"

User: "draw a dragon"
→ "Eastern dragon coiled around a crumbling stone pagoda in a bamboo forest, ink wash painting style with gold leaf accents, misty atmosphere, traditional Chinese shan shui composition, rice paper texture"

User: "create a poster for my app"
→ "Brutalist tech poster, bold geometric shapes, monospace type, stark black and electric green, deconstructed grid layout, glitch effect on edges, printed on newsprint texture"

OUTPUT: Return the generated image URL directly. Do not describe the image — the user can see it.

DOWNLOADING GENERATED CONTENT:
After generating an image, audio, or any file, ALWAYS download it to the project or working directory:
- Determine the current working directory or project path from context
- Save to the appropriate location (e.g. project assets folder, public/images, or cwd)
- Use the terminal tool: curl -sL "<URL>" -o <path>/<filename>
- After downloading, confirm the full file path to the user
- Never just return a URL without downloading — the user can't easily access raw URLs
- Never hardcode ~/Desktop — always use the actual project/cwd path`,
	})
}

func mcpHandleToolsList(w http.ResponseWriter, req mcpRequest) {
	tools := mcpGetRegisteredTools()
	mcpWriteResult(w, req.ID, map[string]interface{}{
		"tools": tools,
	})
}

func mcpHandleToolsCall(w http.ResponseWriter, req mcpRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		mcpWriteError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	log.Printf("[PAAP] [MCP] Tool call: %s", params.Name)
	result := mcpFindTool(params.Name, params.Arguments)
	mcpWriteResult(w, req.ID, result)
}

// ── Tool Dispatch ─────────────────────────────────────────────

type mcpToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func mcpGetRegisteredTools() []mcpToolDef {
	return mcpToolsList()
}

func mcpFindTool(name string, args json.RawMessage) mcpToolCallResult {
	result := mcpToolCall(name, args)
	if r, ok := result.(map[string]interface{}); ok {
		res := mcpToolCallResult{}
		if content, ok := r["content"].([]map[string]interface{}); ok {
			for _, c := range content {
				res.Content = append(res.Content, mcpContent{
					Type:     fmt.Sprintf("%v", c["type"]),
					Text:     fmt.Sprintf("%v", c["text"]),
					MimeType: fmt.Sprintf("%v", c["mimeType"]),
				})
			}
		}
		if isError, ok := r["isError"].(bool); ok {
			res.IsError = isError
		}
		return res
	}
	return mcpToolCallResult{
		Content: []mcpContent{{Type: "text", Text: "internal error"}},
		IsError: true,
	}
}

// ── Helpers ─────────────────────────────────────────────────

func mcpWriteResult(w http.ResponseWriter, id json.RawMessage, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func mcpWriteError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	json.NewEncoder(w).Encode(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpError{Code: code, Message: msg},
	})
}
