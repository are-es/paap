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
