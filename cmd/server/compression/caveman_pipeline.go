// Package compression implements deterministic, token-saving transforms
// that run on tool output before the request is sent to the LLM provider.
package compression

import (
	"fmt"
	"regexp"
	"strings"
)

// ToolBudget defines head/tail line budgets for a specific tool type.
type ToolBudget struct {
	MaxLines  int
	HeadLines int
	TailLines int
}

// DefaultToolBudgets are the per-tool line budgets.
var DefaultToolBudgets = map[string]ToolBudget{
	"bash": {MaxLines: 30, HeadLines: 20, TailLines: 10},
	"read": {MaxLines: 80, HeadLines: 50, TailLines: 30},
	"grep": {MaxLines: 50, HeadLines: 30, TailLines: 20},
	"find": {MaxLines: 30, HeadLines: 20, TailLines: 10},
	"ls":   {MaxLines: 30, HeadLines: 20, TailLines: 10},
}

// FallbackBudget is used when the tool type is unknown.
var FallbackBudget = ToolBudget{MaxLines: 40, HeadLines: 25, TailLines: 15}

// GetToolBudget returns the budget for a tool, falling back to FallbackBudget.
func GetToolBudget(toolName string) ToolBudget {
	if b, ok := DefaultToolBudgets[toolName]; ok {
		return b
	}
	return FallbackBudget
}

// FlintChipper truncates text using per-tool budget (head+tail preservation).
func FlintChipper(text, toolName string) string {
	budget := GetToolBudget(toolName)
	lines := strings.Split(text, "\n")
	if len(lines) <= budget.MaxLines {
		return text
	}

	omitted := len(lines) - budget.HeadLines - budget.TailLines
	head := lines[:budget.HeadLines]
	tail := lines[len(lines)-budget.TailLines:]

	var sb strings.Builder
	for i, l := range head {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(l)
	}
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("[... %d lines omitted (%s budget: %d) ...]", omitted, toolName, budget.MaxLines))
	sb.WriteString("\n")
	for _, l := range tail {
		sb.WriteByte('\n')
		sb.WriteString(l)
	}

	return sb.String()
}

// ansiEscapeRE matches ANSI/VT100 escape sequences.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripAnsi removes ANSI escape codes from text.
func StripAnsi(text string) string {
	return ansiEscapeRE.ReplaceAllString(text, "")
}

// blankCollapseRE matches 3+ consecutive blank lines.
var blankCollapseRE = regexp.MustCompile(`(\r?\n){3,}`)

// CollapseBlanks collapses 3+ consecutive blank lines into one.
func CollapseBlanks(text string) string {
	return blankCollapseRE.ReplaceAllString(text, "\n\n")
}
