package compression

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25Extractive compresses text by scoring segments with BM25 and keeping top ones.
// Pure heuristic — no ML model, no external dependencies.
// Splits text into sentences, scores each by relevance, keeps top segments
// until target ratio is reached.
func BM25Extractive(text string, targetRatio float64) string {
	if targetRatio >= 1.0 || len(text) < 200 {
		return text
	}

	// Split into segments (sentences or logical blocks)
	segments := splitSegments(text)
	if len(segments) <= 2 {
		return text
	}

	// Target: keep this many segments
	keepCount := max(2, int(float64(len(segments))*targetRatio))

	// Build term frequencies from entire document
	tf := buildTermFreq(text)
	avgLen := avgSegmentLen(segments)

	// Score each segment
	type scored struct {
		idx   int
		score float64
		text  string
	}
	scored_segments := make([]scored, len(segments))
	for i, seg := range segments {
		scored_segments[i] = scored{
			idx:   i,
			score: bm25Score(seg, tf, avgLen, len(segments)),
			text:  seg,
		}
	}

	// Sort by score descending
	sort.Slice(scored_segments, func(i, j int) bool {
		return scored_segments[i].score > scored_segments[j].score
	})

	// Keep top N, restore original order
	kept := make(map[int]bool)
	for i := 0; i < keepCount && i < len(scored_segments); i++ {
		kept[scored_segments[i].idx] = true
	}

	var result []string
	for i, seg := range segments {
		if kept[i] {
			result = append(result, seg)
		}
	}

	compressed := strings.Join(result, "\n")
	// Only return compressed if it actually saves space
	if len(compressed) < len(text) {
		return compressed
	}
	return text
}

// splitSegments splits text into logical segments (paragraphs, sentences, or lines).
func splitSegments(text string) []string {
	// Try splitting by double newline first (paragraphs)
	if paragraphs := strings.Split(text, "\n\n"); len(paragraphs) > 3 {
		var result []string
		for _, p := range paragraphs {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
		if len(result) > 3 {
			return result
		}
	}

	// Fall back to line-based splitting
	lines := strings.Split(text, "\n")
	var result []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

// tokenize splits text into lowercase words for BM25 scoring.
func tokenize(text string) []string {
	var tokens []string
	var current strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// buildTermFreq builds term frequency map from entire document.
func buildTermFreq(text string) map[string]int {
	tf := make(map[string]int)
	for _, token := range tokenize(text) {
		if len(token) > 2 { // skip very short words
			tf[token]++
		}
	}
	return tf
}

// avgSegmentLen returns average token count across segments.
func avgSegmentLen(segments []string) float64 {
	if len(segments) == 0 {
		return 1
	}
	total := 0
	for _, s := range segments {
		total += len(tokenize(s))
	}
	return float64(total) / float64(len(segments))
}

// bm25Score scores a segment using BM25 formula.
// k1=1.5, b=0.75 (standard parameters).
func bm25Score(segment string, docTF map[string]int, avgLen float64, totalDocs int) float64 {
	const k1 = 1.5
	const b = 0.75

	tokens := tokenize(segment)
	if len(tokens) == 0 {
		return 0
	}

	segTF := make(map[string]int)
	for _, t := range tokens {
		segTF[t]++
	}

	score := 0.0
	segLen := float64(len(tokens))

	for term, freq := range segTF {
		docFreq, exists := docTF[term]
		if !exists || docFreq == 0 {
			continue
		}

		// IDF: log((N - n + 0.5) / (n + 0.5) + 1)
		// N = total segments (approx), n = segments containing term (approx: use docFreq)
		n := float64(docFreq)
		N := float64(totalDocs)
		idf := math.Log((N-n+0.5)/(n+0.5) + 1.0)
		if idf < 0 {
			idf = 0
		}

		// TF normalization
		tfNorm := (float64(freq) * (k1 + 1)) / (float64(freq) + k1*(1-b+b*segLen/avgLen))

		score += idf * tfNorm
	}

	// Bonus for segments with code patterns (function, class, import, etc.)
	codeKeywords := []string{"func ", "def ", "class ", "import ", "function ", "const ", "var ", "type ", "struct ", "interface "}
	for _, kw := range codeKeywords {
		if strings.Contains(segment, kw) {
			score *= 1.3
			break
		}
	}

	// Bonus for segments with numbers/data (likely important)
	digitCount := 0
	for _, r := range segment {
		if unicode.IsDigit(r) {
			digitCount++
		}
	}
	if digitCount > 3 {
		score *= 1.1
	}

	return score
}
