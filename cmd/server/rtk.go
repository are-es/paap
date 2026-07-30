package main

import (
	"log"
	"sync"
)

var (
	rtkAvailable = false
	rtkOnce      sync.Once
)

// IsRTKAvailable checks if RTK is installed
func IsRTKAvailable() bool {
	rtkOnce.Do(func() {
		// RTK pipe mode doesn't actually compress, just passes through
		// Disabled until proper filter integration is implemented
		rtkAvailable = false
		log.Printf("[PAAP] RTK integration disabled (pipe mode doesn't compress)")
	})
	return rtkAvailable
}

// CompressToolOutputs is placeholder for RTK compression
func CompressToolOutputs(messages []map[string]interface{}, level string) []map[string]interface{} {
	return messages
}
