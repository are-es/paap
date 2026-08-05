package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	trafficLogFile *os.File
	trafficLogger  *log.Logger
	trafficOnce    sync.Once
)

func initTrafficLogger() {
	trafficOnce.Do(func() {
		dataDir := os.Getenv("PAAP_DATA")
		if dataDir == "" {
			dataDir = filepath.Join(os.Getenv("HOME"), ".paap")
		}
		logDir := filepath.Join(dataDir, "logs")
		os.MkdirAll(logDir, 0755)
		logPath := filepath.Join(logDir, "traffic.log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("[PAAP] Failed to open traffic log: %v", err)
			return
		}
		trafficLogFile = f
		trafficLogger = log.New(f, "", 0)
		log.Printf("[PAAP] Traffic logger started: %s", logPath)
	})
}

// TrafficLog logs a full request/response cycle with compression info
func TrafficLog(entry TrafficEntry) {
	initTrafficLogger()
	if trafficLogger == nil {
		return
	}

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] model=%s provider=%s status=%d latency=%dms | req_raw=%d req_compressed=%d req_saved=%d (%.1f%%) | resp_raw=%d resp_compressed=%d resp_saved=%d (%.1f%%) | compress_mode=%s overhead=%dms ttfb=%dms | stream=%v tokens_in=%d tokens_out=%d",
		ts,
		entry.Model,
		entry.Provider,
		entry.StatusCode,
		entry.LatencyMs,
		entry.ReqRawBytes,
		entry.ReqCompressedBytes,
		entry.ReqSavedBytes,
		entry.ReqSavedPct,
		entry.RespRawBytes,
		entry.RespCompressedBytes,
		entry.RespSavedBytes,
		entry.RespSavedPct,
		entry.CompressMode,
		entry.PAAPOverheadMs,
		entry.TTFBMs,
		entry.IsStream,
		entry.TokensIn,
		entry.TokensOut,
	)
	trafficLogger.Println(line)
}

type TrafficEntry struct {
	Model               string
	Provider            string
	StatusCode          int
	LatencyMs           int64
	ReqRawBytes         int
	ReqCompressedBytes  int
	ReqSavedBytes       int
	ReqSavedPct         float64
	RespRawBytes        int
	RespCompressedBytes int
	RespSavedBytes      int
	RespSavedPct        float64
	CompressMode        string
	PAAPOverheadMs      int64
	TTFBMs              int64
	IsStream            bool
	TokensIn            int
	TokensOut           int
}
