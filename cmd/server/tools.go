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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Enabled     bool   `json:"enabled"`
	RouteModel  string `json:"route_model"`  // Model to route to when triggered
	Priority    int    `json:"priority"`     // Higher = checked first
	Config      string `json:"config"`       // JSON config specific to tool type
}

// ToolRow is used for scanning from SQLite (enabled is int, not bool)
type ToolRow struct {
	ID          string
	Name        string
	Type        string
	Enabled     int
	RouteModel  string
	Priority    int
	Config      string
}

func (r *ToolRow) toTool() *Tool {
	return &Tool{
		ID:         r.ID,
		Name:       r.Name,
		Type:       r.Type,
		Enabled:    r.Enabled == 1,
		RouteModel: r.RouteModel,
		Priority:   r.Priority,
		Config:     r.Config,
	}
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
// Returns the model to route to, or empty string if no tool matches
func ProcessTools(rawBody map[string]interface{}) string {
	tools := GetActiveTools()
	if len(tools) == 0 {
		return ""
	}

	messages, _ := rawBody["messages"].([]interface{})
	if len(messages) == 0 {
		return ""
	}

	for _, tool := range tools {
		if !tool.Enabled {
			continue
		}

		switch tool.Type {
		case ToolTypeVision:
			if hasImageContent(messages) {
				log.Printf("[PAAP] [TOOLS] Vision tool triggered — routing to %s", tool.RouteModel)
				return tool.RouteModel
			}
		// Future tools can be added here
		// case ToolTypeWebSearch:
		//     if hasSearchQuery(messages) {
		//         return tool.RouteModel
		//     }
		}
	}

	return ""
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
	var t Tool
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
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
	if err := CreateTool(&t); err != nil {
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
		var t Tool
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeError(w, 400, "invalid JSON")
			return
		}
		t.ID = id
		if err := UpdateTool(&t); err != nil {
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
