package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Headroom auto-start / stop / monitor. PAAP manages the headroom subprocess
// when the user toggles headroom_enabled=true. The monitor goroutine restarts
// headroom if it crashes while the toggle is on.

const (
	headroomBinaryPath = "/mnt/hdd/venv/bin/headroom"
	headroomHost       = "127.0.0.1"
	headroomPort       = "8787"
	headroomMonitorInt = 10 * time.Second // check interval
	headroomStopGrace  = 5 * time.Second  // SIGTERM wait before SIGKILL
)

var (
	headroomCmd   *exec.Cmd
	headroomCmdMu sync.Mutex
	headroomStop  chan struct{} // closed to stop the monitor goroutine
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
	return headroomBinaryPath // fallback constant
}

// isHeadroomRunning checks if headroom responds to HTTP on its port.
func isHeadroomRunning() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:%s/health", headroomHost, headroomPort))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// startHeadroom launches the headroom subprocess. Returns error if binary
// missing or process fails to start. Non-blocking: the process runs in
// background; use isHeadroomRunning() to confirm it's ready.
func startHeadroom() error {
	if !isHeadroomInstalled() {
		return fmt.Errorf("headroom not installed — run: pip install \"headroom-ai[all]\"")
	}

	headroomCmdMu.Lock()
	defer headroomCmdMu.Unlock()

	// Already running?
	if headroomCmd != nil && headroomCmd.Process != nil {
		// Check if still alive
		if headroomCmd.Process.Signal(syscall.Signal(0)) == nil {
			return nil // already running
		}
		// Dead — clean up
		headroomCmd = nil
	}

	// Resolve host/port from settings (fall back to constants)
	host := getSettingStrCached("headroom_host", headroomHost)
	port := getSettingStrCached("headroom_port", headroomPort)

	cmd := exec.Command(headroomBinaryPathResolved(), "proxy", "--host", host, "--port", port)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start headroom: %w", err)
	}

	headroomCmd = cmd
	log.Printf("[PAAP] Headroom started (pid=%d) at %s:%s", cmd.Process.Pid, host, port)

	// Wait in background so we don't zombie
	go func() {
		err := cmd.Wait()
		headroomCmdMu.Lock()
		if headroomCmd == cmd {
			headroomCmd = nil
		}
		headroomCmdMu.Unlock()
		if err != nil {
			log.Printf("[PAAP] Headroom exited: %v", err)
		} else {
			log.Printf("[PAAP] Headroom exited cleanly")
		}
	}()

	return nil
}

// stopHeadroom kills the headroom process gracefully: SIGTERM, wait 5s, SIGKILL.
func stopHeadroom() {
	headroomCmdMu.Lock()
	cmd := headroomCmd
	headroomCmdMu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	log.Printf("[PAAP] Stopping headroom (pid=%d)...", cmd.Process.Pid)

	// SIGTERM first
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("[PAAP] SIGTERM failed: %v, sending SIGKILL", err)
		cmd.Process.Kill()
		return
	}

	// Wait up to headroomStopGrace for clean exit
	done := make(chan struct{})
	go func() {
		// ProcessState gets populated by the Wait() goroutine in startHeadroom
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			if cmd.Process.Signal(syscall.Signal(0)) != nil {
				close(done)
				return
			}
		}
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[PAAP] Headroom stopped gracefully")
	case <-time.After(headroomStopGrace):
		log.Printf("[PAAP] Headroom didn't exit in %v, sending SIGKILL", headroomStopGrace)
		cmd.Process.Kill()
	}

	headroomCmdMu.Lock()
	if headroomCmd == cmd {
		headroomCmd = nil
	}
	headroomCmdMu.Unlock()
}

// monitorHeadroom is a long-running goroutine that checks headroom health
// every headroomMonitorInt seconds and restarts it if it crashed.
// Call startHeadroomMonitor() to launch; the returned channel can be closed
// to stop monitoring.
func monitorHeadroom(stop <-chan struct{}) {
	ticker := time.NewTicker(headroomMonitorInt)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			log.Printf("[PAAP] Headroom monitor stopped")
			return
		case <-ticker.C:
			if getSettingStrCached("headroom_enabled", "false") != "true" {
				continue
			}
			if isHeadroomRunning() {
				continue
			}
			log.Printf("[PAAP] Headroom not responding, restarting...")
			stopHeadroom()
			if err := startHeadroom(); err != nil {
				log.Printf("[PAAP] Headroom restart failed: %v", err)
			}
		}
	}
}

// startHeadroomMonitor launches the monitor goroutine if not already running.
func startHeadroomMonitor() {
	headroomCmdMu.Lock()
	if headroomStop != nil {
		headroomCmdMu.Unlock()
		return // already running
	}
	stop := make(chan struct{})
	headroomStop = stop
	headroomCmdMu.Unlock()

	go monitorHeadroom(stop)
	log.Printf("[PAAP] Headroom monitor started (check every %v)", headroomMonitorInt)
}

// stopHeadroomMonitor stops the monitor goroutine.
func stopHeadroomMonitor() {
	headroomCmdMu.Lock()
	if headroomStop != nil {
		close(headroomStop)
		headroomStop = nil
	}
	headroomCmdMu.Unlock()
}

// initHeadroomOnStartup is called once at startup. If headroom_enabled=true,
// it auto-starts headroom and begins monitoring.
func initHeadroomOnStartup() {
	if getSettingStrCached("headroom_enabled", "false") != "true" {
		return
	}
	if err := startHeadroom(); err != nil {
		log.Printf("[PAAP] Headroom auto-start failed: %v", err)
		return
	}
	startHeadroomMonitor()
}

// headroomToggle is called from the settings handler when headroom_enabled
// changes. newEnabled is the post-toggle value.
func headroomToggle(newEnabled bool) {
	if newEnabled {
		if err := startHeadroom(); err != nil {
			log.Printf("[PAAP] Headroom toggle-on failed: %v", err)
			return
		}
		startHeadroomMonitor()
		// Auto-disable RTK when headroom is on
		if getSettingStrCached("rtk_enabled", "false") == "true" {
			setSetting("rtk_enabled", "false")
			log.Printf("[PAAP] RTK auto-disabled (headroom active)")
		}
	} else {
		stopHeadroomMonitor()
		stopHeadroom()
	}
}

// shutdownHeadroom is called on server shutdown to clean up the subprocess.
func shutdownHeadroom() {
	stopHeadroomMonitor()
	stopHeadroom()
}
