package main

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var (
	rtkBin       string
	rtkOnce      sync.Once
	rtkAvailable = false
)

// findRTKBinary locates the rtk binary
func findRTKBinary() string {
	rtkOnce.Do(func() {
		// Check common locations
		locations := []string{
			"/usr/local/bin/rtk",
			"/usr/bin/rtk",
			os.Getenv("HOME") + "/.local/bin/rtk",
			"/home/dolvin/.local/bin/rtk",
		}

		for _, loc := range locations {
			if _, err := exec.LookPath(loc); err == nil {
				rtkBin = loc
				rtkAvailable = true
				log.Printf("[PAAP] RTK found at %s", rtkBin)
				return
			}
		}

		// Try which
		if path, err := exec.LookPath("rtk"); err == nil {
			rtkBin = path
			rtkAvailable = true
			log.Printf("[PAAP] RTK found at %s", rtkBin)
			return
		}

		log.Printf("[PAAP] RTK not found — tool output compression disabled")
	})

	return rtkBin
}

// IsRTKAvailable checks if RTK is installed
func IsRTKAvailable() bool {
	findRTKBinary()
	return rtkAvailable
}

// detectCommand detects what command was run from tool result content
func detectCommand(content string) string {
	contentLower := strings.ToLower(content)
	
	// Git log patterns
	if strings.Contains(contentLower, "commit ") && strings.Contains(contentLower, "author:") {
		return "git"
	}
	if strings.Contains(contentLower, "fix:") || strings.Contains(contentLower, "feat:") {
		// Looks like git log --oneline
		return "git"
	}
	
	// Diff patterns
	if strings.HasPrefix(content, "---") || strings.HasPrefix(content, "+++") {
		return "diff"
	}
	if strings.Contains(content, "@@") && strings.Contains(content, "-") {
		return "diff"
	}
	
	// Test output patterns
	if strings.Contains(contentLower, "pass") && strings.Contains(contentLower, "fail") {
		return "test"
	}
	if strings.Contains(contentLower, "ok") && strings.Contains(contentLower, "error") {
		return "test"
	}
	
	// Error patterns
	if strings.Contains(contentLower, "error:") || strings.Contains(contentLower, "warning:") {
		return "err"
	}
	
	// JSON patterns
	if strings.HasPrefix(strings.TrimSpace(content), "{") || strings.HasPrefix(strings.TrimSpace(content), "[") {
		return "json"
	}
	
	// File listing patterns (ls -la)
	if strings.Contains(content, "total ") && strings.Contains(content, "drwx") {
		return "ls"
	}
	
	// Grep patterns
	if strings.Contains(content, ":") && strings.Contains(content, "Binary") {
		return "grep"
	}
	
	return "pipe"
}

// CompressToolOutput compresses a single tool output using RTK
func CompressToolOutput(content string, level string) string {
	if !IsRTKAvailable() {
		return content
	}

	// Skip small outputs (< 200 chars)
	if len(content) < 200 {
		return content
	}

	// Detect command type
	cmdType := detectCommand(content)
	log.Printf("[PAAP] RTK detected command type: %s (%d chars)", cmdType, len(content))

	// Build RTK command
	var args []string
	switch cmdType {
	case "git":
		args = []string{"git", "log", "--oneline", "-20"}
	case "diff":
		args = []string{"diff"}
	case "test":
		args = []string{"test"}
	case "err":
		args = []string{"err"}
	case "json":
		args = []string{"json"}
	case "ls":
		args = []string{"ls"}
	case "grep":
		args = []string{"grep"}
	default:
		args = []string{"pipe"}
		if level == "ultra" {
			args = append(args, "--ultra-compact")
		}
	}

	// For non-pipe commands, we need to run the command through RTK
	// But since we have the output already, we'll use pipe with appropriate filtering
	// RTK's pipe command applies general filtering
	
	// Use pipe with ultra-compact for better compression
	args = []string{"pipe"}
	if level == "ultra" {
		args = append(args, "--ultra-compact")
	}

	cmd := exec.Command(rtkBin, args...)
	cmd.Stdin = strings.NewReader(content)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		// Log error but return original content
		log.Printf("[PAAP] RTK compression failed: %v (%s)", err, errOut.String())
		return content
	}

	compressed := out.String()

	// If compression didn't help (or made it larger), return original
	if len(compressed) >= len(content) {
		return content
	}

	saved := len(content) - len(compressed)
	percent := float64(saved) / float64(len(content)) * 100
	log.Printf("[PAAP] RTK compressed tool output: %d → %d bytes (%.1f%% saved)", len(content), len(compressed), percent)

	return compressed
}


// ClientUsesRTK checks if the client is using RTK by looking at tool call commands
func ClientUsesRTK(messages []map[string]interface{}) bool {
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "assistant" {
			continue
		}
		
		toolCalls, ok := msg["tool_calls"].([]interface{})
		if !ok {
			continue
		}
		
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			
			fn, ok := tcMap["function"].(map[string]interface{})
			if !ok {
				continue
			}
			
			args, ok := fn["arguments"].(string)
			if !ok {
				continue
			}
			
			// Check if command starts with rtk
			if strings.Contains(args, `"command":`) {
				// Extract command from JSON args
				if strings.Contains(args, `"rtk `) || strings.Contains(args, `"rtk"`) {
					log.Printf("[PAAP] Detected client RTK usage in tool call")
					return true
				}
			}
		}
	}
	
	return false
}

// CompressToolOutputs compresses all tool result messages in the messages array
func CompressToolOutputs(messages []map[string]interface{}, level string) []map[string]interface{} {
	if !IsRTKAvailable() {
		return messages
	}

	compressed := 0
	for i, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}

		// Get content
		content, ok := msg["content"].(string)
		if !ok {
			continue
		}

		// Compress
		newContent := CompressToolOutput(content, level)
		if newContent != content {
			messages[i]["content"] = newContent
			compressed++
		}
	}

	if compressed > 0 {
		log.Printf("[PAAP] RTK compressed %d tool outputs", compressed)
	}

	return messages
}
