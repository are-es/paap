package compression

// Level defines the compression aggressiveness.
type Level int

const (
	// LevelOff disables compression entirely.
	LevelOff Level = iota
	// LevelLite: ANSI strip, whitespace collapse, dedup identical lines.
	LevelLite
	// LevelMedium: structural (JSON minify, tabular, dedup) + Headroom.
	LevelMedium
	// LevelHigh: full pipeline (Headroom + Caveman + BM25 + new strategies).
	LevelHigh
)

var levelNames = map[string]Level{
	"off":    LevelOff,
	"lite":   LevelLite,
	"medium": LevelMedium,
	"high":   LevelHigh,
}

// ParseLevel converts a config string to a Level.
func ParseLevel(s string) Level {
	if l, ok := levelNames[s]; ok {
		return l
	}
	return LevelMedium
}

// String returns the lowercase name of a Level.
func (l Level) String() string {
	switch l {
	case LevelOff:
		return "off"
	case LevelLite:
		return "lite"
	case LevelMedium:
		return "medium"
	case LevelHigh:
		return "high"
	default:
		return "medium"
	}
}

// levelConfig holds per-level tunables.
type levelConfig struct {
	// Head/tail line budget for FlintChipper truncation.
	HeadLines int
	TailLines int
	// MaxLines is the hard cap after truncation.
	MaxLines int
	// RunANSI strips ANSI escape codes.
	RunANSI bool
	// RunBlankCollapse collapses 3+ consecutive blank lines.
	RunBlankCollapse bool
	// RunFlintChipper enables line-budget truncation.
	RunFlintChipper bool
	// RunProseFilter enables filler-word removal (caveman rules).
	RunProseFilter bool
	// RunLogDedup deduplicates repeated log lines.
	RunLogDedup bool
	// RunStringTruncate caps long strings inside structured output.
	RunStringTruncate bool
	// RunBM25 enables BM25 extractive scoring for text compression.
	RunBM25 bool
	// BM25TargetRatio is the target ratio for BM25 (0.5 = keep 50% of segments).
	BM25TargetRatio float64
	// MinCompressSize is the minimum byte threshold to trigger compression.
	MinCompressSize int
}

var levelConfigs = map[Level]levelConfig{
	LevelOff: {
		MinCompressSize: 0,
	},
	LevelLite: {
		// ANSI strip, whitespace collapse, dedup
		RunANSI:          true,
		RunBlankCollapse: true,
		MinCompressSize:  50,
	},
	LevelMedium: {
		// Structural: JSON minify, tabular, dedup + Headroom
		RunANSI:          true,
		RunBlankCollapse: true,
		MinCompressSize:  200,
	},
	LevelHigh: {
		// Full pipeline: Headroom + Caveman + BM25 + new strategies
		HeadLines:         60,
		TailLines:         30,
		MaxLines:          250,
		RunANSI:           true,
		RunBlankCollapse:  true,
		RunFlintChipper:   true,
		RunProseFilter:    true,
		RunLogDedup:       true,
		RunStringTruncate: true,
		RunBM25:           true,
		BM25TargetRatio:   0.5,
		MinCompressSize:   50,
	},
}

func getConfig(l Level) levelConfig {
	if c, ok := levelConfigs[l]; ok {
		return c
	}
	return levelConfigs[LevelMedium]
}
