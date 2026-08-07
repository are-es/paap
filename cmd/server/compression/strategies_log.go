package compression

import (
	"fmt"
	"strings"
)

// DeduplicateLogLines removes consecutive repeated lines and appends a count.
// Repeated sequences of 2+ identical lines become:
//   [previous line] (×N)
//
// This is the native replacement for rtk's log filter.
func DeduplicateLogLines(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return s
	}

	var out []string
	prev := lines[0]
	count := 1

	for _, line := range lines[1:] {
		if line == prev {
			count++
			continue
		}
		out = append(out, formatDup(prev, count))
		prev = line
		count = 1
	}
	out = append(out, formatDup(prev, count))
	return strings.Join(out, "\n")
}

func formatDup(line string, count int) string {
	if count > 1 {
		return fmt.Sprintf("%s (×%d)", line, count)
	}
	return line
}

// TruncateHeadTail keeps head + tail lines, replacing the middle with a marker.
func TruncateHeadTail(s string, head, tail int) string {
	lines := strings.Split(s, "\n")
	total := head + tail + 5 // +5 for marker and margin
	if len(lines) <= total {
		return s
	}

	var out []string
	out = append(out, lines[:head]...)
	out = append(out, fmt.Sprintf("\n... [%d lines omitted] ...\n", len(lines)-head-tail))
	if tail > 0 {
		out = append(out, lines[len(lines)-tail:]...)
	}
	return strings.Join(out, "\n")
}

// HardCapLines truncates content to maxLines, keeping only the head.
func HardCapLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") +
		fmt.Sprintf("\n... [truncated to %d lines] ...\n", maxLines)
}

// IsLogLike detects if content looks like log output (timestamps, log levels).
var logPrefixes = []string{
	"INFO ", "WARN ", "ERROR ", "DEBUG ", "TRACE ",
	"[INFO]", "[WARN]", "[ERROR]", "[DEBUG]",
}

func IsLogLike(s string) bool {
	lines := strings.Split(s, "\n")
	check := 10
	if len(lines) < check {
		check = len(lines)
	}
	logLines := 0
	for i := 0; i < check; i++ {
		line := strings.TrimSpace(lines[i])
		for _, prefix := range logPrefixes {
			if strings.HasPrefix(line, prefix) {
				logLines++
				break
			}
		}
	}
	return logLines >= 3
}

// ApplyLogStrategy applies dedup + truncation for the given level config.
func ApplyLogStrategy(s string, cfg levelConfig) string {
	if len(s) < cfg.MinCompressSize {
		return s
	}

	if cfg.RunLogDedup {
		s = DeduplicateLogLines(s)
	}

	if cfg.RunFlintChipper {
		s = TruncateHeadTail(s, cfg.HeadLines, cfg.TailLines)
	}

	if cfg.MaxLines > 0 {
		s = HardCapLines(s, cfg.MaxLines)
	}

	return s
}
