package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type CompressionConfig struct {
	Name        string
	Description string
	Shared      map[string]string
	Levels      map[string]string
}

var (
	compressionConfigs map[string]*CompressionConfig
	compressionMu      sync.Once
)

// loadCompressionConfigs loads caveman.md and ponytail.md from config/
func loadCompressionConfigs() map[string]*CompressionConfig {
	compressionMu.Do(func() {
		compressionConfigs = make(map[string]*CompressionConfig)

		configDir := findConfigDir()
		if configDir == "" {
			log.Printf("[PAAP] Config directory not found, using fallback prompts")
			return
		}

		for _, name := range []string{"caveman", "ponytail"} {
			filePath := filepath.Join(configDir, name+".md")
			data, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("[PAAP] Failed to load %s: %v", filePath, err)
				continue
			}

			config := parseMarkdown(string(data), name)
			compressionConfigs[name] = config
			log.Printf("[PAAP] Loaded compression config: %s (%d levels, %d shared rules)", 
				name, len(config.Levels), len(config.Shared))
		}
	})
	return compressionConfigs
}

// parseMarkdown parses a Markdown file into a CompressionConfig
func parseMarkdown(content, name string) *CompressionConfig {
	config := &CompressionConfig{
		Name:   name,
		Shared: make(map[string]string),
		Levels: make(map[string]string),
	}

	lines := strings.Split(content, "\n")
	var currentSection string
	var currentSubSection string
	var buffer []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		
		// Check for headers
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			// Main title - skip
			continue
		}
		
		if strings.HasPrefix(trimmed, "## ") {
			// Save previous section
			if currentSection != "" && currentSubSection != "" {
				saveSection(config, currentSection, currentSubSection, strings.Join(buffer, "\n"))
			}
			currentSection = strings.TrimPrefix(trimmed, "## ")
			currentSubSection = ""
			buffer = nil
			continue
		}
		
		if strings.HasPrefix(trimmed, "### ") {
			// Save previous subsection
			if currentSection != "" && currentSubSection != "" {
				saveSection(config, currentSection, currentSubSection, strings.Join(buffer, "\n"))
			}
			currentSubSection = strings.TrimPrefix(trimmed, "### ")
			buffer = nil
			continue
		}

		// Add content to buffer
		if currentSection != "" && currentSubSection != "" {
			buffer = append(buffer, line)
		}
	}

	// Save last section
	if currentSection != "" && currentSubSection != "" {
		saveSection(config, currentSection, currentSubSection, strings.Join(buffer, "\n"))
	}

	return config
}

// saveSection saves content to the appropriate config field
func saveSection(config *CompressionConfig, section, subSection, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	switch section {
	case "Levels":
		config.Levels[strings.ToLower(subSection)] = content
	case "Shared Rules":
		config.Shared[strings.ToLower(subSection)] = content
	}
}

// findConfigDir finds the config directory
func findConfigDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		configDir := filepath.Join(dir, "..", "config")
		if _, err := os.Stat(configDir); err == nil {
			return configDir
		}
	}

	candidates := []string{
		filepath.Join(os.Getenv("HOME"), ".paap", "config"),
		"/etc/paap/config",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}

// GetCompressionPrompt returns the full instruction text for a mode and level
func GetCompressionPrompt(mode, level string) string {
	configs := loadCompressionConfigs()

	config, ok := configs[mode]
	if !ok {
		return ""
	}

	// Get level instruction
	levelInstruction, ok := config.Levels[level]
	if !ok {
		levelInstruction = config.Levels["full"]
	}

	// Build full prompt with shared rules
	var parts []string

	if mode == "caveman" {
		parts = append(parts, levelInstruction)
		parts = append(parts, config.Shared["examples"])
		parts = append(parts, config.Shared["boundaries"])
		parts = append(parts, config.Shared["auto clarity"])
		parts = append(parts, config.Shared["persistence"])
		parts = append(parts, config.Shared["no invented abbreviations"])
		parts = append(parts, config.Shared["preserve language"])
		parts = append(parts, config.Shared["no self reference"])
		parts = append(parts, config.Shared["no decoration"])
	} else if mode == "ponytail" {
		parts = append(parts, config.Shared["persona"])
		parts = append(parts, levelInstruction)
		parts = append(parts, config.Shared["ladder"])
		parts = append(parts, config.Shared["rules"])
		parts = append(parts, config.Shared["output"])
		parts = append(parts, config.Shared["not lazy"])
		parts = append(parts, config.Shared["persistence"])
	}

	return strings.Join(parts, " ")
}

