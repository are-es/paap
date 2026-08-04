package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/dolvin/paap/internal/db"
)

// ── Tool Types ──────────────────────────────────────────
const (
	ToolTypeVision = "vision"
	// Future: ToolTypeWebSearch = "web_search"
	// Future: ToolTypeCodeExec = "code_execution"
)

// ── Tool Definition ─────────────────────────────────────
type Tool struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Enabled     bool     `json:"enabled"`
	RouteModel  string   `json:"route_model"`  // JSON array string: ["model1","model2"]
	RouteModels []string `json:"route_models"` // Parsed model chain
	Priority    int      `json:"priority"`
	Config      string   `json:"config"`
}

// ToolRow is used for scanning from SQLite (enabled is int, not bool)
type ToolRow struct {
	ID         string
	Name       string
	Type       string
	Enabled    int
	RouteModel string
	Priority   int
	Config     string
}

// ParseRouteModels parses the JSON array in RouteModel into RouteModels slice
func (t *Tool) ParseRouteModels() {
	t.RouteModels = nil
	rm := strings.TrimSpace(t.RouteModel)
	if rm == "" {
		return
	}
	// Try JSON array first
	if strings.HasPrefix(rm, "[") {
		var models []string
		if err := json.Unmarshal([]byte(rm), &models); err == nil {
			t.RouteModels = models
			return
		}
	}
	// Fallback: treat as single model string
	t.RouteModels = []string{rm}
}

func (r *ToolRow) toTool() *Tool {
	t := &Tool{
		ID:         r.ID,
		Name:       r.Name,
		Type:       r.Type,
		Enabled:    r.Enabled == 1,
		RouteModel: r.RouteModel,
		Priority:   r.Priority,
		Config:     r.Config,
	}
	t.ParseRouteModels()
	return t
}

// ToolMatch holds the result of tool detection: which tool triggered and the fallback model chain
type ToolMatch struct {
	ToolName string
	Models   []string // Ordered fallback chain
}

// ── Tool System ─────────────────────────────────────────
var (
	toolsCache     []*Tool
	toolsCacheDirty = true
)

// LoadTools refreshes the tools cache from database
func LoadTools() {
	rows, err := db.DB.Query("SELECT id, name, type, enabled, route_model, priority, config FROM tools WHERE enabled=1 ORDER BY priority DESC")
	if err != nil {
		log.Printf("[PAAP] [TOOLS] Failed to load tools: %v", err)
		return
	}
	defer rows.Close()

	var tools []*Tool
	for rows.Next() {
		var r ToolRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.Enabled, &r.RouteModel, &r.Priority, &r.Config); err != nil {
			continue
		}
		tools = append(tools, r.toTool())
	}
	toolsCache = tools
	toolsCacheDirty = false
	log.Printf("[PAAP] [TOOLS] Loaded %d active tools", len(tools))
}

// GetActiveTools returns cached active tools
func GetActiveTools() []*Tool {
	if toolsCacheDirty {
		LoadTools()
	}
	return toolsCache
}

// InvalidateToolsCache marks cache as dirty
func InvalidateToolsCache() {
	toolsCacheDirty = true
}

// ── Vision Detection ────────────────────────────────────

// hasImageContent checks if any message contains image blocks
func hasImageContent(messages []interface{}) bool {
	for _, msg := range messages {
		mm, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := mm["content"].([]interface{})
		if !ok {
			continue
		}
		for _, block := range content {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			// OpenAI format: image_url
			if bm["type"] == "image_url" {
				return true
			}
			// Anthropic format: image with source
			if bm["type"] == "image" {
				if _, ok := bm["source"].(map[string]interface{}); ok {
					return true
				}
			}
			// Base64 data URI in text (fallback detection)
			if bm["type"] == "text" {
				if text, ok := bm["text"].(string); ok {
					if strings.Contains(text, "data:image/") {
						return true
					}
				}
			}
		}
	}
	return false
}

// ── Tool Processing ─────────────────────────────────────

// ProcessTools checks if any tool should handle the request
// Returns ToolMatch with model chain, or nil if no tool matches
func ProcessTools(rawBody map[string]interface{}) *ToolMatch {
	tools := GetActiveTools()
	if len(tools) == 0 {
		return nil
	}

	messages, _ := rawBody["messages"].([]interface{})
	if len(messages) == 0 {
		return nil
	}

	for _, tool := range tools {
		if !tool.Enabled {
			continue
		}

		switch tool.Type {
		case ToolTypeVision:
			if hasImageContent(messages) {
				models := tool.RouteModels
				if len(models) == 0 {
					models = []string{tool.RouteModel}
				}
				log.Printf("[PAAP] [TOOLS] Vision tool triggered — fallback chain: %v", models)
				return &ToolMatch{ToolName: tool.Name, Models: models}
			}
		// Future tools can be added here
		// case ToolTypeWebSearch:
		//     if hasSearchQuery(messages) {
		//         return tool.RouteModel
		//     }
		}
	}

	return nil
}

// ── Tool CRUD ───────────────────────────────────────────

// ListTools returns all tools from database
func ListTools() ([]*Tool, error) {
	rows, err := db.DB.Query("SELECT id, name, type, enabled, route_model, priority, config FROM tools ORDER BY priority DESC, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []*Tool
	for rows.Next() {
		var r ToolRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.Enabled, &r.RouteModel, &r.Priority, &r.Config); err != nil {
			continue
		}
		tools = append(tools, r.toTool())
	}
	return tools, nil
}

// GetTool returns a single tool by ID
func GetTool(id string) (*Tool, error) {
	var r ToolRow
	err := db.DB.QueryRow("SELECT id, name, type, enabled, route_model, priority, config FROM tools WHERE id=?", id).
		Scan(&r.ID, &r.Name, &r.Type, &r.Enabled, &r.RouteModel, &r.Priority, &r.Config)
	if err != nil {
		return nil, err
	}
	return r.toTool(), nil
}

// CreateTool adds a new tool
func CreateTool(t *Tool) error {
	_, err := db.DB.Exec(
		"INSERT INTO tools (id, name, type, enabled, route_model, priority, config) VALUES (?, ?, ?, ?, ?, ?, ?)",
		t.ID, t.Name, t.Type, t.Enabled, t.RouteModel, t.Priority, t.Config,
	)
	if err == nil {
		InvalidateToolsCache()
	}
	return err
}

// UpdateTool updates an existing tool
func UpdateTool(t *Tool) error {
	_, err := db.DB.Exec(
		"UPDATE tools SET name=?, type=?, enabled=?, route_model=?, priority=?, config=? WHERE id=?",
		t.Name, t.Type, t.Enabled, t.RouteModel, t.Priority, t.Config, t.ID,
	)
	if err == nil {
		InvalidateToolsCache()
	}
	return err
}

// DeleteTool removes a tool
func DeleteTool(id string) error {
	_, err := db.DB.Exec("DELETE FROM tools WHERE id=?", id)
	if err == nil {
		InvalidateToolsCache()
	}
	return err
}

// ── Tool API Handlers ───────────────────────────────────

// normalizeToolRouteModel reads raw JSON body, converts route_model from
// array/string to a consistent JSON array string, then re-decodes into Tool.
func decodeToolFromBody(r *http.Request) (*Tool, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, err
	}
	// Normalize route_model: if it's a JSON array, keep as-is string; if plain string, wrap in array
	if rmRaw, ok := raw["route_model"]; ok {
		rmStr := string(rmRaw)
		if strings.HasPrefix(strings.TrimSpace(rmStr), "\"") {
			// Plain JSON string — wrap in array
			var s string
			json.Unmarshal(rmRaw, &s)
			arr, _ := json.Marshal([]string{s})
			raw["route_model"] = json.RawMessage(`"` + strings.ReplaceAll(string(arr), `"`, `\"`) + `"`)
		} else if strings.HasPrefix(strings.TrimSpace(rmStr), "[") {
			// Already array — store as JSON string in the field
			raw["route_model"] = json.RawMessage(`"` + strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(rmStr), `"`, `\"`), "\n", "") + `"`)
		}
	}
	// Re-marshal and decode into Tool
	b, _ := json.Marshal(raw)
	var t Tool
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	t.ParseRouteModels()
	return &t, nil
}

func toolListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}
	tools, err := ListTools()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, tools)
}

func toolCreateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}
	t, err := decodeToolFromBody(r)
	if err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if t.ID == "" {
		t.ID = genID()
	}
	if t.Name == "" || t.Type == "" {
		writeError(w, 400, "name and type are required")
		return
	}
	if err := CreateTool(t); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, t)
}

func toolRoutes(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tools/")
	if id == "" {
		writeError(w, 400, "tool ID required")
		return
	}

	switch r.Method {
	case "GET":
		t, err := GetTool(id)
		if err != nil {
			writeError(w, 404, "tool not found")
			return
		}
		writeJSON(w, t)

	case "PUT":
		t, err := decodeToolFromBody(r)
		if err != nil {
			writeError(w, 400, "invalid JSON")
			return
		}
		t.ID = id
		if err := UpdateTool(t); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, t)

	case "DELETE":
		if err := DeleteTool(id); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]string{"status": "deleted"})

	default:
		writeError(w, 405, "method not allowed")
	}
}
