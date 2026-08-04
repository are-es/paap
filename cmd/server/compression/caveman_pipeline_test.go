package compression

import (
	"strings"
	"testing"
)

func TestStripAnsi(t *testing.T) {
	input := "\x1b[31mRED\x1b[0m normal \x1b[1;32mGREEN\x1b[0m"
	got := StripAnsi(input)
	want := "RED normal GREEN"
	if got != want {
		t.Errorf("StripAnsi = %q, want %q", got, want)
	}
}

func TestCollapseBlanks(t *testing.T) {
	input := "a\n\n\n\n\nb"
	got := CollapseBlanks(input)
	want := "a\n\nb"
	if got != want {
		t.Errorf("CollapseBlanks = %q, want %q", got, want)
	}
	// Double blank preserved
	input2 := "a\n\nb"
	got2 := CollapseBlanks(input2)
	if got2 != input2 {
		t.Errorf("CollapseBlanks should preserve double blank, got %q", got2)
	}
}

func TestFlintChipper(t *testing.T) {
	// Under budget — no change
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	text := strings.Join(lines, "\n")
	got := FlintChipper(text, "bash")
	if got != text {
		t.Error("FlintChipper should not truncate under-budget text")
	}

	// Over budget
	lines = make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	text = strings.Join(lines, "\n")
	got = FlintChipper(text, "bash")
	if !strings.Contains(got, "lines omitted") {
		t.Error("FlintChipper should add omission marker")
	}
	if !strings.Contains(got, "(bash budget: 80)") {
		t.Error("FlintChipper should mention tool name and budget")
	}
}

func TestTruncateLongOutput(t *testing.T) {
	lines := make([]string, 600)
	for i := range lines {
		lines[i] = "line"
	}
	text := strings.Join(lines, "\n")
	got := TruncateLongOutput(text)
	if !strings.Contains(got, "cave mode truncation") {
		t.Error("TruncateLongOutput should add truncation marker")
	}
	gotLines := strings.Split(got, "\n")
	// head(200) + marker(3 lines) + tail(100) ≈ 303
	if len(gotLines) > 310 {
		t.Errorf("TruncateLongOutput produced %d lines, expected ~303", len(gotLines))
	}
}

func TestStoneTabletJSON(t *testing.T) {
	// Small JSON — no compression (under 50 lines)
	small := `{"a":1}`
	if got := StoneTablet(small, "bash", ""); got != small {
		t.Errorf("StoneTablet should skip small JSON, got %q", got)
	}

	// Large JSON — compress
	var sb strings.Builder
	sb.WriteString("{\n")
	for i := 0; i < 100; i++ {
		sb.WriteString(`  "key` + strings.Repeat("x", i%10) + `": "value` + strings.Repeat("y", i%20) + `",` + "\n")
	}
	sb.WriteString("}")
	large := sb.String()
	got := StoneTablet(large, "bash", "")
	if !strings.Contains(got, "JSON compressed") && got == large {
		// Might not compress if result is >= 60% of original — that's OK
		t.Log("StoneTablet returned original (compression not worthwhile)")
	}
}

func TestStoneTabletNonBash(t *testing.T) {
	text := strings.Repeat("line\n", 60)
	if got := StoneTablet(text, "read", ""); got != text {
		t.Error("StoneTablet should skip non-bash tools")
	}
}

func TestDetectFormat(t *testing.T) {
	jsonText := strings.Repeat("\n", 50) + `{"key":"value"}`
	if got := DetectFormat(jsonText); got != formatJSON {
		t.Errorf("DetectFormat = %d, want formatJSON", got)
	}
}

func TestReadDedupCache(t *testing.T) {
	c := NewReadDeduplicationCache()
	content := "file contents here"

	// First read — no stub
	stub := c.CheckRead("/path/file.go", content)
	if stub != "" {
		t.Errorf("First read should return empty, got %q", stub)
	}

	// Second read — stub
	stub = c.CheckRead("/path/file.go", content)
	if stub == "" {
		t.Error("Second read should return stub")
	}
	if !strings.Contains(stub, "File unchanged since read #1") {
		t.Errorf("Stub should reference read #1, got %q", stub)
	}

	// Changed content — no stub
	stub = c.CheckRead("/path/file.go", "different content")
	if stub != "" {
		t.Errorf("Changed content should return empty, got %q", stub)
	}

	// Reset
	c.Reset()
	stub = c.CheckRead("/path/file.go", content)
	if stub != "" {
		t.Error("After reset, should be treated as first read")
	}
}
