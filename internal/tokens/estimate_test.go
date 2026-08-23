package tokens

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestEstimateByteClasses checks the estimator responds to character class
// rather than treating every byte as 1/4 token. Bounds are deliberately loose:
// this is a heuristic and the test guards the shape of the answer, not a
// specific tokenizer's output.
func TestEstimateByteClasses(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		minTok       int
		maxTok       int
		beatsNaive   bool // estimate should differ from len/4
		naiveAllowed bool
	}{
		{
			name:         "english prose",
			input:        "The quick brown fox jumps over the lazy dog and keeps running.",
			minTok:       10,
			maxTok:       25,
			naiveAllowed: true,
		},
		{
			name:         "indonesian prose",
			input:        "Sistem ini menghitung token untuk setiap permintaan yang masuk ke proxy.",
			minTok:       10,
			maxTok:       25,
			naiveAllowed: true,
		},
		{
			// 20 CJK characters = 60 UTF-8 bytes. len/4 says 15 tokens, but CJK
			// is roughly one token per character, so the real answer is ~20-30.
			name:       "cjk chinese",
			input:      strings.Repeat("测试字符", 5),
			minTok:     20,
			maxTok:     45,
			beatsNaive: true,
		},
		{
			name:       "japanese kana",
			input:      strings.Repeat("こんにちは世界", 4),
			minTok:     18,
			maxTok:     50,
			beatsNaive: true,
		},
		{
			name:       "korean hangul",
			input:      strings.Repeat("안녕하세요", 5),
			minTok:     15,
			maxTok:     50,
			beatsNaive: true,
		},
		{
			name:         "minified json",
			input:        `{"id":"abc","count":42,"nested":{"a":1,"b":2},"list":[1,2,3,4,5]}`,
			minTok:       10,
			maxTok:       40,
			naiveAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Estimate(tt.input)
			if got < tt.minTok || got > tt.maxTok {
				t.Errorf("Estimate(%q) = %d, want between %d and %d",
					truncate(tt.input), got, tt.minTok, tt.maxTok)
			}
			naive := len(tt.input) / 4
			if tt.beatsNaive && got == naive {
				t.Errorf("Estimate = %d, identical to naive len/4 = %d; byte-class awareness had no effect", got, naive)
			}
		})
	}
}

// TestEstimateDenseBlob asserts a base64 payload is charged more tokens than the
// same byte count of prose, since encoded data fragments harder under BPE.
func TestEstimateDenseBlob(t *testing.T) {
	raw := strings.Repeat("payload-bytes", 40)
	blob := base64.StdEncoding.EncodeToString([]byte(raw))

	// Prose of the same length, with word breaks.
	prose := strings.Repeat("word ", len(blob)/5+1)
	prose = prose[:len(blob)]

	blobTok := Estimate(blob)
	proseTok := Estimate(prose)

	if blobTok <= proseTok {
		t.Errorf("base64 blob (%d bytes) = %d tokens, prose (%d bytes) = %d tokens; dense data must cost more",
			len(blob), blobTok, len(prose), proseTok)
	}
}

// TestEstimateProseNotTreatedAsDense guards the run-length threshold: normal
// words must not be classified as encoded blobs just because they are alphanumeric.
func TestEstimateProseNotTreatedAsDense(t *testing.T) {
	prose := "this is an ordinary sentence with short words only"
	got := Estimate(prose)
	naive := len(prose) / 4
	// Prose should land close to the ASCII ratio.
	if got < naive-2 || got > naive+2 {
		t.Errorf("Estimate(prose) = %d, want ~%d (ASCII ratio); short words leaked into the dense bucket", got, naive)
	}
}

// TestEstimateEdgeCases covers empty and tiny inputs.
func TestEstimateEdgeCases(t *testing.T) {
	if got := Estimate(""); got != 0 {
		t.Errorf("Estimate(\"\") = %d, want 0", got)
	}
	for _, s := range []string{"a", "ab", "x y"} {
		if got := Estimate(s); got < 1 {
			t.Errorf("Estimate(%q) = %d, want at least 1", s, got)
		}
	}
}

// TestEstimateDeterministic asserts repeated calls agree; the estimator must not
// depend on map iteration or any other unordered state.
func TestEstimateDeterministic(t *testing.T) {
	inputs := []string{
		"mixed content 测试 with base64 " + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 100))),
		`{"volatile":"none"}`,
		strings.Repeat("日本語テキスト", 10),
	}
	for _, in := range inputs {
		first := Estimate(in)
		for i := 0; i < 50; i++ {
			if got := Estimate(in); got != first {
				t.Fatalf("Estimate(%q) returned %d then %d", truncate(in), first, got)
			}
		}
	}
}

func truncate(s string) string {
	if len(s) <= 40 {
		return s
	}
	return s[:40] + "..."
}
