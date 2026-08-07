package compression

// Level defines the compression aggressiveness.
type Level int

const (
	// LevelOff disables compression entirely.
	LevelOff Level = iota
	// LevelLite: 5 oldest tool outputs. ANSI strip + blank collapse.
	LevelLite
	// LevelMedium: 10 oldest tool + user messages. +line budget +prose +dedup.
	LevelMedium
	// LevelHigh: 15 oldest tool + user + system. +JSON/XML +aggressive trunc.
	LevelHigh
)

var levelNames = map[string]Level{
	"off":    LevelOff,
	"lite":   LevelLite,
	"medium": LevelMedium,
	"high":   LevelHigh,
}

// ParseLevel converts a config string to a Level.
// Unknown values fall back to LevelMedium.
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
	// RunStoneTablet enables JSON/XML semantic compression.
	RunStoneTablet bool
	// RunProseFilter enables filler-word removal (caveman rules).
	RunProseFilter bool
	// RunLogDedup deduplicates repeated log lines.
	RunLogDedup bool
	// RunStringTruncate caps long strings inside structured output.
	RunStringTruncate bool
	// MinCompressSize is the minimum byte threshold to trigger compression.
	MinCompressSize int
}

var levelConfigs = map[Level]levelConfig{
	LevelOff: {
		MinCompressSize: 0,
	},
	LevelLite: {
		RunANSI:          true,
		RunBlankCollapse: true,
		MinCompressSize:  200,
	},
	LevelMedium: {
		HeadLines:        120,
		TailLines:        60,
		MaxLines:         500,
		RunANSI:          true,
		RunBlankCollapse: true,
		RunFlintChipper:  true,
		RunProseFilter:   true,
		RunLogDedup:      true,
		MinCompressSize:  200,
	},
	LevelHigh: {
		HeadLines:         60,
		TailLines:         30,
		MaxLines:          250,
		RunANSI:           true,
		RunBlankCollapse:  true,
		RunFlintChipper:   true,
		RunStoneTablet:    true,
		RunProseFilter:    true,
		RunLogDedup:       true,
		RunStringTruncate: true,
		MinCompressSize:   200,
	},
}

func getConfig(l Level) levelConfig {
	if c, ok := levelConfigs[l]; ok {
		return c
	}
	return levelConfigs[LevelMedium]
}
