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
	input2 := "a\n\nb"
	got2 := CollapseBlanks(input2)
	if got2 != input2 {
		t.Errorf("CollapseBlanks should preserve double blank, got %q", got2)
	}
}

func TestFlintChipper(t *testing.T) {
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "line"
	}
	text := strings.Join(lines, "\n")
	got := FlintChipper(text, "bash")
	if got != text {
		t.Error("FlintChipper should not truncate under-budget text")
	}

	lines = make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	text = strings.Join(lines, "\n")
	got = FlintChipper(text, "bash")
	if !strings.Contains(got, "lines omitted") {
		t.Error("FlintChipper should add omission marker")
	}
	if !strings.Contains(got, "(bash budget: 30)") {
		t.Error("FlintChipper should mention tool name and budget")
	}
}
