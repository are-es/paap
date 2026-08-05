package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	headroomHost       = "127.0.0.1"
	headroomPort       = "8787"
	headroomMonitorInt = 10 * time.Second // check interval
	headroomStopGrace  = 5 * time.Second  // SIGTERM wait before SIGKILL
)

var (
	headroomCmd           *exec.Cmd
	headroomCmdMu         sync.Mutex
	headroomStop          chan struct{} // closed to stop the monitor goroutine
	headroomHealthMu      sync.RWMutex
	headroomHealthy       bool
	headroomHealthKnown   bool
	headroomHealthExpires time.Time
)

// isHeadroomInstalled checks if the headroom binary exists.
func isHeadroomInstalled() bool {
	return findHeadroomBinary() != ""
}

// headroomBinaryPathResolved returns the binary path for subprocess launch.
func headroomBinaryPathResolved() string {
	if p := findHeadroomBinary(); p != "" {
		return p
	}
	// Try to find from environment variable
	if envPath := os.Getenv("HEADROOM_PATH"); envPath != "" {
		return envPath
	}
	return "" // no fallback - require explicit path
}

// startHeadroomServer launches headroom as a managed child process.
func startHeadroomServer() error {
	headroomCmdMu.Lock()
	defer headroomCmdMu.Unlock()

	if headroomCmd != nil && headroomCmd.Process != nil {
		return nil // already running
	}

	binPath := headroomBinaryPathResolved()
	if binPath == "" {
		return fmt.Errorf("headroom binary not found")
	}

	// Create log file
	logsDir := filepath.Join(".", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create logs dir: %w", err)
	}

	logPath := filepath.Join(logsDir, "headroom.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open headroom log: %w", err)
	}

	cmd := exec.Command(binPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HEADROOM_HOST=%s", headroomHost),
		fmt.Sprintf("HEADROOM_PORT=%s", headroomPort),
	)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start headroom: %w", err)
	}

	headroomCmd = cmd
	log.Printf("[headroom] Started server (PID: %d) on %s:%s", cmd.Process.Pid, headroomHost, headroomPort)

	// Wait for process in background
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("[headroom] Process exited: %v", err)
		} else {
			log.Printf("[headroom] Process exited cleanly")
		}
		headroomCmdMu.Lock()
		headroomCmd = nil
		headroomCmdMu.Unlock()
	}()

	return nil
}

// stopHeadroomServer gracefully stops the headroom process.
func stopHeadroomServer() {
	headroomCmdMu.Lock()
	defer headroomCmdMu.Unlock()

	if headroomCmd == nil || headroomCmd.Process == nil {
		return
	}

	log.Printf("[headroom] Stopping server (PID: %d)...", headroomCmd.Process.Pid)

	// Try graceful shutdown first
	if err := headroomCmd.Process.Signal(os.Interrupt); err != nil {
		log.Printf("[headroom] Failed to send interrupt: %v", err)
	}

	// Wait for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- headroomCmd.Wait()
	}()

	select {
	case <-time.After(headroomStopGrace):
		log.Printf("[headroom] Graceful shutdown timeout, killing...")
		if err := headroomCmd.Process.Kill(); err != nil {
			log.Printf("[headroom] Failed to kill: %v", err)
		}
	case err := <-done:
		if err != nil {
			log.Printf("[headroom] Process exited with error: %v", err)
		} else {
			log.Printf("[headroom] Process exited cleanly")
		}
	}

	headroomCmd = nil
}

// monitorHeadroom watches the headroom process and restarts if needed.
func monitorHeadroom(ctx context.Context) {
	ticker := time.NewTicker(headroomMonitorInt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-headroomStop:
			return
		case <-ticker.C:
			headroomCmdMu.Lock()
			running := headroomCmd != nil && headroomCmd.Process != nil
			headroomCmdMu.Unlock()

			if !running {
				log.Printf("[headroom] Monitor: process not running, restarting...")
				if err := startHeadroomServer(); err != nil {
					log.Printf("[headroom] Monitor: restart failed: %v", err)
				}
			}
		}
	}
}

// IsHeadroomRunning checks if headroom is currently running.
func IsHeadroomRunning() bool {
	headroomCmdMu.Lock()
	defer headroomCmdMu.Unlock()
	return headroomCmd != nil && headroomCmd.Process != nil
}

// GetHeadroomStatus returns the current headroom status for API responses.
func GetHeadroomStatus() map[string]interface{} {
	status := map[string]interface{}{
		"installed":    isHeadroomInstalled(),
		"binary_path":  headroomBinaryPathResolved(),
		"running":      IsHeadroomRunning(),
		"host":         headroomHost,
		"port":         headroomPort,
		"architecture": runtime.GOARCH,
		"platform":     runtime.GOOS,
	}

	if IsHeadroomRunning() {
		status["pid"] = headroomCmd.Process.Pid
	}

	return status
}

// GetHeadroomMetrics returns current headroom metrics.
func GetHeadroomMetrics() map[string]interface{} {
	if !IsHeadroomRunning() {
		return map[string]interface{}{
			"running": false,
			"error":   "headroom not running",
		}
	}

	// Try to get metrics from headroom API
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:%s/metrics", headroomHost, headroomPort))
	if err != nil {
		return map[string]interface{}{
			"running": true,
			"error":   fmt.Sprintf("failed to get metrics: %v", err),
		}
	}
	defer resp.Body.Close()

	// Parse response
	var metrics map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return map[string]interface{}{
			"running": true,
			"error":   fmt.Sprintf("failed to parse metrics: %v", err),
		}
	}

	metrics["running"] = true
	return metrics
}

// RestartHeadroom stops and restarts the headroom server.
func RestartHeadroom() error {
	stopHeadroomServer()
	return startHeadroomServer()
}

// RestartHeadroomService restarts the headroom service.
func RestartHeadroomService() error {
	if !isHeadroomInstalled() {
		return fmt.Errorf("headroom not installed")
	}

	stopHeadroomServer()
	return startHeadroomServer()
}

// headroomToggle toggles the headroom server on/off.
func headroomToggle(enabled bool) {
	if enabled {
		if !IsHeadroomRunning() {
			if err := startHeadroomServer(); err != nil {
				log.Printf("[headroom] Failed to start: %v", err)
			}
		}
	} else {
		if IsHeadroomRunning() {
			stopHeadroomServer()
		}
	}
}

// headroomStatus handles the /api/headroom/status endpoint.
func headroomStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := GetHeadroomStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// initHeadroomOnStartup initializes headroom on server startup.
func initHeadroomOnStartup() {
	if isHeadroomInstalled() {
		log.Println("[headroom] Binary found, starting headroom server...")
		if err := startHeadroomServer(); err != nil {
			log.Printf("[headroom] Failed to start: %v", err)
		} else {
			// Start monitor in background
			headroomStop = make(chan struct{})
			go monitorHeadroom(context.Background())
		}
	} else {
		log.Println("[headroom] Binary not found, skipping auto-start")
	}
}
