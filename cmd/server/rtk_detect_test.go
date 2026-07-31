package main

import "testing"

// TestDetectFilter locks in the filter-detection table. Every name returned
// here must exist in `rtk pipe --filter <name>`; an invalid name makes rtk exit
// non-zero and CompressToolOutput silently falls back to uncompressed output.
func TestDetectFilter(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "git log",
			content: "commit a1b2c3d4e5f6789012345678901234567890abcd\nAuthor: Dolvin\nDate: today\n\n    fix: thing\n",
			want:    "git-log",
		},
		{
			name:    "git diff header",
			content: "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n-a\n+b\n",
			want:    "git-diff",
		},
		{
			name:    "bare hunk",
			content: "@@ -10,5 +10,6 @@ func Foo()\n context\n-removed\n+added\n",
			want:    "git-diff",
		},
		{
			name:    "git status",
			content: "On branch master\nChanges not staged for commit:\n\tmodified:   x.go\n",
			want:    "git-status",
		},
		{
			name:    "go test",
			content: "=== RUN   TestFoo\n--- PASS: TestFoo (0.01s)\nok  \texample.com/pkg\t0.02s\n",
			want:    "go-test",
		},
		{
			name:    "go build error",
			content: "# example.com/pkg\ncmd/server/rtk.go:67:17: undefined: rtkDetectWindow\n",
			want:    "go-build",
		},
		{
			name:    "tsc error",
			content: "src/app.tsx(12,5): error TS2304: Cannot find name 'foo'.\n",
			want:    "tsc",
		},
		{
			name:    "grep output",
			content: "src/a.go:12:func Foo() {\nsrc/a.go:44:  return nil\nsrc/b.go:7:package main\n",
			want:    "grep",
		},
		{
			name:    "find output",
			content: "./cmd/server/main.go\n./cmd/server/rtk.go\n./internal/db/db.go\n",
			want:    "find",
		},
		{
			name:    "generic noisy log falls back",
			content: "starting\nloading config\nconnecting\nretry 1\nretry 2\nretry 3\nconnected\nserving\nrequest ok\nrequest ok\n",
			want:    "log",
		},
		{
			name:    "short unstructured prose gets no filter",
			content: "The quick brown fox jumped over the lazy dog and then went home.",
			want:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectFilter(tc.content); got != tc.want {
				t.Errorf("detectFilter() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIsErrorToolResult guards the rule that error traces are never compressed.
func TestIsErrorToolResult(t *testing.T) {
	cases := []struct {
		name string
		msg  map[string]interface{}
		want bool
	}{
		{"is_error true", map[string]interface{}{"is_error": true}, true},
		{"is_error false", map[string]interface{}{"is_error": false}, false},
		{"status error", map[string]interface{}{"status": "error"}, true},
		{"status ok", map[string]interface{}{"status": "ok"}, false},
		{"neither field", map[string]interface{}{"role": "tool"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isErrorToolResult(tc.msg); got != tc.want {
				t.Errorf("isErrorToolResult() = %v, want %v", got, tc.want)
			}
		})
	}
}
