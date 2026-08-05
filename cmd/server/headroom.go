package main

import (
	"container/list"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	headroomMinCompressSize = 1024 // Minimum size in bytes to apply compression
)

// HeadroomTuning represents a provider tuning configuration
type HeadroomTuning struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	SafetyMargin float64   `json:"safety_margin"`
	ScaleFactor  float64   `json:"scale_factor"`
	HeadroomMs   int       `json:"headroom_ms"`
	Priority     int       `json:"priority"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Headroom represents the headroom configuration
type Headroom struct {
	SafetyMargin float64          `json:"safety_margin"`
	ScaleFactor  float64          `json:"scale_factor"`
	HeadroomMs   int              `json:"headroom_ms"`
	Priority     int              `json:"priority"`
	Tunings      []HeadroomTuning `json:"tunings"`
}

// HeadroomManager manages the headroom configuration
type HeadroomManager struct {
	configPath string
	config     *Headroom
}

// NewHeadroomManager creates a new headroom manager
func NewHeadroomManager(configPath string) *HeadroomManager {
	return &HeadroomManager{
		configPath: configPath,
		config: &Headroom{
			SafetyMargin: 0.1,
			ScaleFactor:  1.0,
			HeadroomMs:   100,
			Priority:     1,
		},
	}
}

// Load loads the headroom configuration from disk
func (m *HeadroomManager) Load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.Save()
		}
		return fmt.Errorf("failed to read headroom config: %w", err)
	}

	if err := json.Unmarshal(data, m.config); err != nil {
		return fmt.Errorf("failed to parse headroom config: %w", err)
	}

	return nil
}

// Save saves the headroom configuration to disk
func (m *HeadroomManager) Save() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal headroom config: %w", err)
	}

	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write headroom config: %w", err)
	}

	return nil
}

// GetConfig returns the current headroom configuration
func (m *HeadroomManager) GetConfig() *Headroom {
	return m.config
}

// UpdateConfig updates the headroom configuration
func (m *HeadroomManager) UpdateConfig(config *Headroom) error {
	m.config = config
	return m.Save()
}

// GetTuning returns the tuning for a specific provider/model
func (m *HeadroomManager) GetTuning(provider, model string) *HeadroomTuning {
	for _, tuning := range m.config.Tunings {
		if tuning.Provider == provider && tuning.Model == model {
			return &tuning
		}
	}
	return nil
}

// SetTuning sets a tuning configuration
func (m *HeadroomManager) SetTuning(tuning HeadroomTuning) error {
	for i, t := range m.config.Tunings {
		if t.Provider == tuning.Provider && t.Model == tuning.Model {
			m.config.Tunings[i] = tuning
			return m.Save()
		}
	}

	m.config.Tunings = append(m.config.Tunings, tuning)
	return m.Save()
}

// DeleteTuning deletes a tuning configuration
func (m *HeadroomManager) DeleteTuning(provider, model string) error {
	for i, tuning := range m.config.Tunings {
		if tuning.Provider == provider && tuning.Model == model {
			m.config.Tunings = append(m.config.Tunings[:i], m.config.Tunings[i+1:]...)
			return m.Save()
		}
	}

	return fmt.Errorf("tuning not found: %s/%s", provider, model)
}

// ResetTuning resets a tuning to defaults
func (m *HeadroomManager) ResetTuning(provider, model string) error {
	for i, tuning := range m.config.Tunings {
		if tuning.Provider == provider && tuning.Model == model {
			m.config.Tunings[i] = HeadroomTuning{
				ID:           tuning.ID,
				Provider:     provider,
				Model:        model,
				SafetyMargin: 0.1,
				ScaleFactor:  1.0,
				HeadroomMs:   100,
				Priority:     1,
				Enabled:      true,
				CreatedAt:    tuning.CreatedAt,
				UpdatedAt:    time.Now(),
			}
			return m.Save()
		}
	}

	return fmt.Errorf("tuning not found: %s/%s", provider, model)
}

// ListTunings returns all tunings sorted by provider and model
func (m *HeadroomManager) ListTunings() []HeadroomTuning {
	tunings := make([]HeadroomTuning, len(m.config.Tunings))
	copy(tunings, m.config.Tunings)

	sort.Slice(tunings, func(i, j int) bool {
		if tunings[i].Provider != tunings[j].Provider {
			return tunings[i].Provider < tunings[j].Provider
		}
		return tunings[i].Model < tunings[j].Model
	})

	return tunings
}

// findHeadroomBinary searches common locations for the headroom binary.
func findHeadroomBinary() string {
	// Check if headroom is in PATH
	if path, err := exec.LookPath("headroom"); err == nil {
		return path
	}

	// Check common installation locations
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "headroom"),
		filepath.Join(os.Getenv("HOME"), "go", "bin", "headroom"),
		filepath.Join("/usr", "local", "bin", "headroom"),
		filepath.Join("/usr", "bin", "headroom"),
	}

	// Also check current directory's venv
	if cwd, err := os.Getwd(); err == nil {
		venvPath := filepath.Join(cwd, "venv", "bin", "headroom")
		candidates = append(candidates, venvPath)
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// hrCacheSlot represents a cached item in the headroom cache
type hrCacheSlot struct {
	key      string
	data     []byte
	bytes    int
	expiry   time.Time
}

// hrCacheStore is the global headroom cache store
var hrCacheStore = struct {
	mu   sync.RWMutex
	by   map[string]hrCacheSlot
	lru  *list.List
	bytes int64
}{
	by:  make(map[string]hrCacheSlot),
	lru: list.New(),
}

// headroomResponse represents the response from headroom compression
type headroomResponse struct {
	Messages []map[string]interface{} `json:"messages"`
}

// headroomRequest represents a request to headroom compression
type headroomRequest struct {
	Messages []map[string]interface{} `json:"messages"`
	Model    string                  `json:"model"`
}

// compressEndpoint returns the compress endpoint URL for the given headroom base URL.
func compressEndpoint(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + "/v1/compress"
	}

	// Ensure path ends with /
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.Path += "v1/compress"

	return u.String()
}

// CompressWithHeadroom compresses messages with headroom awareness.
func CompressWithHeadroom(msgs []map[string]interface{}, model string) []map[string]interface{} {
	if len(msgs) == 0 {
		return msgs
	}

	// Get headroom manager
	hm := NewHeadroomManager("config/headroom.json")
	hm.Load()

	// Get tuning for model
	tuning := hm.GetTuning("openai", model)
	if tuning == nil || !tuning.Enabled {
		return msgs
	}

	// Apply compression based on tuning
	result := make([]map[string]interface{}, len(msgs))
	copy(result, msgs)

	for i, msg := range result {
		if content, ok := msg["content"].(string); ok {
			if len(content) >= headroomMinCompressSize {
				compressed := CompressToolOutput(content, "caveman")
				if tuning.SafetyMargin > 0.5 {
					compressed = CompressToolOutput(compressed, "caveman")
				}
				result[i]["content"] = compressed
			}
		}
	}

	return result
}
