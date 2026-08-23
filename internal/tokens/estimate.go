// Package tokens provides a single shared token-count estimator.
//
// PAAP previously estimated tokens with a bare len(s)/4 in five separate places
// (compression stats, compression log API, the router's savings figure, the
// Anthropic message_start placeholder, and the provider playground). Those
// numbers were presented alongside provider-reported counts with nothing marking
// them as guesses, and 4 bytes/token is badly wrong for anything that is not
// ASCII prose.
//
// This estimator stays a heuristic on purpose: it exists to size compression
// savings and to fill a placeholder before the real usage arrives. Anything
// derived from it must be flagged as estimated (see tokens_estimated in the logs
// table). Do not use it for billing.
package tokens

import "unicode/utf8"

// Bytes-per-token ratios by character class. Values are the observed averages
// for BPE tokenizers used by the OpenAI/Anthropic/Gemini families.
const (
	// asciiBytesPerToken: English prose and code, ~4 bytes per token.
	asciiBytesPerToken = 4.0
	// cjkBytesPerToken: CJK characters are 3 UTF-8 bytes and roughly 1 token
	// each, sometimes 2 characters per token. ~2 bytes per token is a safe mean.
	cjkBytesPerToken = 2.0
	// denseBytesPerToken: base64, hex, and long unbroken identifier runs
	// fragment into more tokens than prose, ~3 bytes per token.
	denseBytesPerToken = 3.0
)

// denseRunThreshold is the length in bytes at which an unbroken run of
// base64/hex-ish characters stops looking like a word and starts looking like an
// encoded blob. Ordinary words and identifiers fall well under it.
const denseRunThreshold = 24

// Estimate returns an approximate token count for s.
//
// It is an ESTIMATE, never a measurement: callers must set tokens_estimated=1 on
// anything derived from it. Returns 0 for the empty string so callers can
// distinguish "nothing" from "a little".
func Estimate(s string) int {
	if s == "" {
		return 0
	}

	var asciiBytes, cjkBytes, denseBytes int
	// runBytes accumulates the current unbroken run of dense-candidate bytes. It
	// is charged to the dense bucket only if the run grows past the threshold,
	// otherwise it is ordinary prose.
	runBytes := 0

	closeRun := func() {
		if runBytes >= denseRunThreshold {
			denseBytes += runBytes
		} else {
			asciiBytes += runBytes
		}
		runBytes = 0
	}

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size

		switch {
		case isDenseCandidate(r):
			runBytes += size
		case isCJK(r):
			closeRun()
			cjkBytes += size
		default:
			closeRun()
			asciiBytes += size
		}
	}
	closeRun()

	est := float64(asciiBytes)/asciiBytesPerToken +
		float64(cjkBytes)/cjkBytesPerToken +
		float64(denseBytes)/denseBytesPerToken

	if est < 1 {
		return 1
	}
	return int(est)
}

// isCJK reports whether r is in a CJK, Hiragana, Katakana, or Hangul block.
func isCJK(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Ext A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // Hangul syllables
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility
		return true
	case r >= 0x20000 && r <= 0x2FA1F: // CJK Ext B-F
		return true
	}
	return false
}

// isDenseCandidate reports whether r can be part of a base64/hex/identifier run.
// Only ASCII alphanumerics and the base64 alphabet qualify — whitespace and
// punctuation break a run, which is what keeps ordinary prose out of the dense
// bucket.
func isDenseCandidate(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '+' || r == '/' || r == '=' || r == '_' || r == '-':
		return true
	}
	return false
}
