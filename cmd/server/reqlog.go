package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	reqLog     *log.Logger
	reqLogOnce sync.Once
	reqLogFile *os.File
	reqLogPath string
	reqLogMu   sync.Mutex
)

const maxReqLogSize = 2 * 1024 * 1024 // 2MB

func initReqLog() {
	reqLogOnce.Do(func() {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".paap", "logs")
		os.MkdirAll(dir, 0755)

		reqLogPath = filepath.Join(dir, "requests.log")
		openReqLog()
	})
}

func openReqLog() {
	f, err := os.OpenFile(reqLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[REQLOG] Failed to open %s: %v", reqLogPath, err)
		return
	}
	reqLogFile = f
	reqLog = log.New(f, "", 0)
	log.Printf("[REQLOG] Request logging to %s", reqLogPath)
}

// rotateIfNeeded checks file size and truncates if over 2MB
func rotateIfNeeded() {
	if reqLogFile == nil {
		return
	}
	info, err := reqLogFile.Stat()
	if err != nil {
		return
	}
	if info.Size() < maxReqLogSize {
		return
	}
	// Close, truncate, reopen
	reqLogFile.Close()
	f, err := os.OpenFile(reqLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("[REQLOG] Failed to truncate %s: %v", reqLogPath, err)
		return
	}
	reqLogFile = f
	reqLog = log.New(f, "", 0)
	log.Printf("[REQLOG] Rotated log (was %d bytes)", info.Size())
}

// LogRequest logs the incoming client request to the request log file.
func LogRequest(method, path, clientKey string, body map[string]interface{}) {
	initReqLog()
	if reqLog == nil {
		return
	}

	reqLogMu.Lock()
	defer reqLogMu.Unlock()
	rotateIfNeeded()

	ts := time.Now().Format("2006-01-02T15:04:05Z")
	model, _ := body["model"].(string)
	stream, _ := body["stream"].(bool)

	// Trim messages for logging — keep role + content preview only
	var msgSummary []map[string]interface{}
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if mm, ok := m.(map[string]interface{}); ok {
				role, _ := mm["role"].(string)
				entry := map[string]interface{}{"role": role}
				switch content := mm["content"].(type) {
				case string:
					if len(content) > 200 {
						entry["content"] = content[:200] + "..."
					} else {
						entry["content"] = content
					}
				case []interface{}:
					entry["content_parts"] = len(content)
				}
				msgSummary = append(msgSummary, entry)
			}
		}
	}

	// Build log entry
	entry := map[string]interface{}{
		"timestamp": ts,
		"direction": "→",
		"method":    method,
		"path":      path,
		"client":    clientKey,
		"model":     model,
		"stream":    stream,
		"messages":  msgSummary,
	}

	// Include other params (tools, max_tokens, temperature, etc.)
	for k, v := range body {
		if k == "messages" || k == "model" || k == "stream" {
			continue
		}
		entry[k] = v
	}

	data, _ := json.MarshalIndent(entry, "", "  ")
	reqLog.Printf("→ REQUEST %s\n%s\n", ts, string(data))
}

// LogResponse logs the outgoing response to the request log file.
func LogResponse(statusCode int, latencyMs int64, tokensIn, tokensOut int, provider, keyName, keyValue, errMsg string, retryCount int) {
	initReqLog()
	if reqLog == nil {
		return
	}

	reqLogMu.Lock()
	defer reqLogMu.Unlock()
	rotateIfNeeded()

	ts := time.Now().Format("2006-01-02T15:04:05Z")

	status := fmt.Sprintf("%d", statusCode)
	if statusCode == 504 {
		status = "504 TIMEOUT"
	} else if statusCode == 0 {
		status = "0 (unknown)"
	}

	line := fmt.Sprintf("← RESPONSE %s | %s | %dms | %d in / %d out | provider=%s key=%s (%s)",
		ts, status, latencyMs, tokensIn, tokensOut, provider, keyName, maskKey(keyValue))

	if errMsg != "" {
		line += fmt.Sprintf(" | error=%s", errMsg)
	}
	if retryCount > 0 {
		line += fmt.Sprintf(" | retries=%d", retryCount)
	}

	reqLog.Println(line)
}


