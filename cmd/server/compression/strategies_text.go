package compression

import (
	"regexp"
	"strings"
)

// proseRule is a compiled filler-removal pattern with its replacement.
type proseRule struct {
	Pattern *regexp.Regexp
	Replace string
}

// Compile all prose rules once at init.
// EN + ID filler words, case-insensitive, word-boundary safe.
// Multi-word phrases FIRST, then single words (order matters).
var proseRules = []proseRule{

	// ── ENGLISH: Multi-word phrases (process first) ──────────────

	// Opening pleasantries (sentence-start only)
	{regexp.MustCompile(`(?i)^(sure|certainly|of course|great|wonderful|fantastic|excellent|absolutely|definitely|perfectly|understood)\b[.,!\s]*`), ""},
	{regexp.MustCompile(`(?i)^(I'd be happy to|I'll help you|I can help you|let me help you|I will help you|I'm here to assist you|I'm ready to help)\b[^.]*[.]\s*`), ""},
	{regexp.MustCompile(`(?i)^no problem[.!?\s]*`), ""},
	{regexp.MustCompile(`(?i)^(here is|here are|here's)\s+`), ""},
	{regexp.MustCompile(`(?i)^(let me|allow me to|I'll)\s+(just\s+)?(show you|help you|explain|assist)[^.]*[.]\s*`), ""},

	// Closing filler (sentence-end only)
	{regexp.MustCompile(`(?i)\s*(?:please\s+)?(?:don't hesitate to|feel free to)\s+[^.]+[.]\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*let me know if you (?:need|have|would like)[^.]+[.]\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*if you have any (?:other\s+)?(?:questions|concerns|issues)[^.]+[.]\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*is there anything else I can help (?:you with|with)\??\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*hope this (?:helps|is helpful)[.!]?\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*(?:happy|glad) to (?:help|assist)[.!]?\s*$`), ""},

	// EN filler transition phrases (multi-word)
	{regexp.MustCompile(`(?i)\bat the end of the day\b`), ""},
	{regexp.MustCompile(`(?i)\bwhen it comes down to it\b`), ""},
	{regexp.MustCompile(`(?i)\bthe fact of the matter is\b`), ""},
	{regexp.MustCompile(`(?i)\bthe thing is\b`), ""},
	{regexp.MustCompile(`(?i)\bwhat I'm trying to say is\b`), ""},
	{regexp.MustCompile(`(?i)\bwhat this means is\b`), ""},
	{regexp.MustCompile(`(?i)\bin other words\b`), ""},
	{regexp.MustCompile(`(?i)\bthat being said\b`), ""},
	{regexp.MustCompile(`(?i)\bhaving said that\b`), ""},
	{regexp.MustCompile(`(?i)\bwith that said\b`), ""},
	{regexp.MustCompile(`(?i)\ball things considered\b`), ""},
	{regexp.MustCompile(`(?i)\bfor what it's worth\b`), ""},
	{regexp.MustCompile(`(?i)\bneedless to say\b`), ""},
	{regexp.MustCompile(`(?i)\bit goes without saying\b`), ""},
	{regexp.MustCompile(`(?i)\bas a matter of fact\b`), ""},
	{regexp.MustCompile(`(?i)\bas far as I'm concerned\b`), ""},
	{regexp.MustCompile(`(?i)\bif you ask me\b`), ""},
	{regexp.MustCompile(`(?i)\bmoving on\b`), ""},
	{regexp.MustCompile(`(?i)\bthat said\b`), ""},
	{regexp.MustCompile(`(?i)\bin fact\b`), ""},

	// EN redundant verb phrases (multi-word)
	{regexp.MustCompile(`(?i)\bin order to\b`), "to"},
	{regexp.MustCompile(`(?i)\bdue to the fact that\b`), "because"},
	{regexp.MustCompile(`(?i)\bin the event that\b`), "if"},
	{regexp.MustCompile(`(?i)\bat this point in time\b`), "now"},
	{regexp.MustCompile(`(?i)\bfor the purpose of\b`), "for"},
	{regexp.MustCompile(`(?i)\bin the process of\b`), ""},
	{regexp.MustCompile(`(?i)\bit is important to note that\b`), ""},
	{regexp.MustCompile(`(?i)\bit should be noted that\b`), ""},
	{regexp.MustCompile(`(?i)\bit is worth mentioning that\b`), ""},
	{regexp.MustCompile(`(?i)\bplease note that\b`), ""},
	{regexp.MustCompile(`(?i)\bplease be aware that\b`), ""},
	{regexp.MustCompile(`(?i)\bI want to point out that\b`), ""},
	{regexp.MustCompile(`(?i)\bI'd like to mention that\b`), ""},
	{regexp.MustCompile(`(?i)\blet me just say\b`), ""},
	{regexp.MustCompile(`(?i)\blet me be clear\b`), ""},
	{regexp.MustCompile(`(?i)\bto be honest\b`), ""},
	{regexp.MustCompile(`(?i)\bto be fair\b`), ""},
	{regexp.MustCompile(`(?i)\bto be frank\b`), ""},
	{regexp.MustCompile(`(?i)\bI think that\b`), ""},
	{regexp.MustCompile(`(?i)\bI believe that\b`), ""},
	{regexp.MustCompile(`(?i)\bin my opinion\b`), ""},
	{regexp.MustCompile(`(?i)\bit's worth noting that\b`), ""},
	{regexp.MustCompile(`(?i)\bit's important to note that\b`), ""},
	{regexp.MustCompile(`(?i)\bit should be noted that\b`), ""},
	{regexp.MustCompile(`(?i)\bas mentioned\b`), ""},
	{regexp.MustCompile(`(?i)\bas previously mentioned\b`), ""},
	{regexp.MustCompile(`(?i)\bas stated before\b`), ""},
	{regexp.MustCompile(`(?i)\blike I said\b`), ""},
	{regexp.MustCompile(`(?i)\bas I said\b`), ""},
	{regexp.MustCompile(`(?i)\bonce again\b`), ""},
	{regexp.MustCompile(`(?i)\bonce more\b`), ""},
	{regexp.MustCompile(`(?i)\bpretty much\b`), ""},
	{regexp.MustCompile(`(?i)\bmore or less\b`), ""},
	{regexp.MustCompile(`(?i)\bin a way\b`), ""},
	{regexp.MustCompile(`(?i)\bto some extent\b`), ""},
	{regexp.MustCompile(`(?i)\bI guess\b`), ""},
	{regexp.MustCompile(`(?i)\bI suppose\b`), ""},
	{regexp.MustCompile(`(?i)\bI feel like\b`), ""},
	{regexp.MustCompile(`(?i)\bI mean\b`), ""},
	{regexp.MustCompile(`(?i)\byou know\b`), ""},
	{regexp.MustCompile(`(?i)\byou see\b`), ""},
	{regexp.MustCompile(`(?i)\bas it were\b`), ""},
	{regexp.MustCompile(`(?i)\bso to speak\b`), ""},
	{regexp.MustCompile(`(?i)\bkind of\b`), ""},
	{regexp.MustCompile(`(?i)\bkinda\b`), ""},
	{regexp.MustCompile(`(?i)\bsort of\b`), ""},
	{regexp.MustCompile(`(?i)\bsorta\b`), ""},
	{regexp.MustCompile(`(?i)\bso much\b`), ""},
	{regexp.MustCompile(`(?i)\ba great deal\b`), ""},
	{regexp.MustCompile(`(?i)\ba lot\b`), ""},
	{regexp.MustCompile(`(?i)\bas far as\b`), ""},
	{regexp.MustCompile(`(?i)\bin terms of\b`), ""},
	{regexp.MustCompile(`(?i)\bwith respect to\b`), ""},
	{regexp.MustCompile(`(?i)\bwith regard to\b`), ""},
	{regexp.MustCompile(`(?i)\bin regard to\b`), ""},
	{regexp.MustCompile(`(?i)\bpertaining to\b`), ""},
	{regexp.MustCompile(`(?i)\bin relation to\b`), ""},
	{regexp.MustCompile(`(?i)\bwith reference to\b`), ""},
	{regexp.MustCompile(`(?i)\bin light of\b`), ""},
	{regexp.MustCompile(`(?i)\bin view of\b`), ""},
	{regexp.MustCompile(`(?i)\bin spite of\b`), ""},
	{regexp.MustCompile(`(?i)\bregardless of\b`), ""},
	{regexp.MustCompile(`(?i)\bdespite the fact that\b`), ""},
	{regexp.MustCompile(`(?i)\bnotwithstanding\b`), ""},
	{regexp.MustCompile(`(?i)\bhereafter\b`), ""},
	{regexp.MustCompile(`(?i)\bthereafter\b`), ""},
	{regexp.MustCompile(`(?i)\bthereupon\b`), ""},
	{regexp.MustCompile(`(?i)\bwhereby\b`), ""},
	{regexp.MustCompile(`(?i)\bwherein\b`), ""},
	{regexp.MustCompile(`(?i)\bwhereupon\b`), ""},

	// ── INDONESIAN: Multi-word phrases (process first) ──────────

	// ID opening/transition phrases
	{regexp.MustCompile(`(?i)^jadi\s+`), ""},
	{regexp.MustCompile(`(?i)^nah\s+`), ""},
	{regexp.MustCompile(`(?i)^gini\s+`), ""},
	{regexp.MustCompile(`(?i)^gitu\s+`), ""},
	{regexp.MustCompile(`(?i)^begini\s+`), ""},
	{regexp.MustCompile(`(?i)^begitu\s+`), ""},
	{regexp.MustCompile(`(?i)^nih\s+`), ""},
	{regexp.MustCompile(`(?i)^tuh\s+`), ""},

	// ID filler transition phrases (multi-word)
	{regexp.MustCompile(`(?i)\bpada dasarnya\b`), ""},
	{regexp.MustCompile(`(?i)\bpada intinya\b`), ""},
	{regexp.MustCompile(`(?i)\byang jelas\b`), ""},
	{regexp.MustCompile(`(?i)\byang pasti\b`), ""},
	{regexp.MustCompile(`(?i)\bgimana ya\b`), ""},
	{regexp.MustCompile(`(?i)\bgimana ya bilangnya\b`), ""},
	{regexp.MustCompile(`(?i)\bapa ya\b`), ""},
	{regexp.MustCompile(`(?i)\bgimana gitu\b`), ""},
	{regexp.MustCompile(`(?i)\bkayak gimana ya\b`), ""},
	{regexp.MustCompile(`(?i)\bistilahnya\b`), ""},
	{regexp.MustCompile(`(?i)\bistilah kerennya\b`), ""},
	{regexp.MustCompile(`(?i)\bsingkatnya\b`), ""},
	{regexp.MustCompile(`(?i)\bringkasnya\b`), ""},
	{regexp.MustCompile(`(?i)\bpada akhirnya\b`), ""},
	{regexp.MustCompile(`(?i)\bakhir kata\b`), ""},
	{regexp.MustCompile(`(?i)\bujung-ujungnya\b`), ""},
	{regexp.MustCompile(`(?i)\byaudah\b`), ""},
	{regexp.MustCompile(`(?i)\budahlah\b`), ""},
	{regexp.MustCompile(`(?i)\budah gitu\b`), ""},
	{regexp.MustCompile(`(?i)\bterus gitu\b`), ""},
	{regexp.MustCompile(`(?i)\bpokoknya\b`), ""},
	{regexp.MustCompile(`(?i)\bsebenernya\b`), ""},
	{regexp.MustCompile(`(?i)\bsejujurnya\b`), ""},
	{regexp.MustCompile(`(?i)\bjujur aja\b`), ""},
	{regexp.MustCompile(`(?i)\bjujur saja\b`), ""},
	{regexp.MustCompile(`(?i)\bsetau saya\b`), ""},
	{regexp.MustCompile(`(?i)\bsetahu saya\b`), ""},
	{regexp.MustCompile(`(?i)\bmenurut saya\b`), ""},
	{regexp.MustCompile(`(?i)\bmenurut gua\b`), ""},
	{regexp.MustCompile(`(?i)\bmenurut gue\b`), ""},
	{regexp.MustCompile(`(?i)\bkayanya\b`), ""},
	{regexp.MustCompile(`(?i)\bkyknya\b`), ""},
	{regexp.MustCompile(`(?i)\bkayak nya\b`), ""},
	{regexp.MustCompile(`(?i)\bagak-agak\b`), ""},
	{regexp.MustCompile(`(?i)\bbener-bener\b`), ""},
	{regexp.MustCompile(`(?i)\bbeneran\b`), ""},
	{regexp.MustCompile(`(?i)\bsungguh-sungguh\b`), ""},
	{regexp.MustCompile(`(?i)\bteramat\b`), ""},

	// ID redundant verb phrases
	{regexp.MustCompile(`(?i)\bperlu diketahui bahwa\b`), ""},
	{regexp.MustCompile(`(?i)\bperlu dicatat bahwa\b`), ""},
	{regexp.MustCompile(`(?i)\bperlu diingat bahwa\b`), ""},
	{regexp.MustCompile(`(?i)\bperlu digarisbawahi bahwa\b`), ""},
	{regexp.MustCompile(`(?i)\byang perlu diperhatikan\b`), ""},
	{regexp.MustCompile(`(?i)\byang perlu digarisbawahi\b`), ""},
	{regexp.MustCompile(`(?i)\bseperti yang disebutkan\b`), ""},
	{regexp.MustCompile(`(?i)\bseperti yang dijelaskan\b`), ""},
	{regexp.MustCompile(`(?i)\bseperti yang sudah dibahas\b`), ""},
	{regexp.MustCompile(`(?i)\bseperti disebutkan sebelumnya\b`), ""},
	{regexp.MustCompile(`(?i)\bsekali lagi\b`), ""},
	{regexp.MustCompile(`(?i)\bsekali lagi perlu ditekankan\b`), ""},
	{regexp.MustCompile(`(?i)\bseperti yang gua bilang\b`), ""},
	{regexp.MustCompile(`(?i)\bseperti kata gua tadi\b`), ""},
	{regexp.MustCompile(`(?i)\bkayak yang tadi gua bilang\b`), ""},
	{regexp.MustCompile(`(?i)\budah gua bilang\b`), ""},
	{regexp.MustCompile(`(?i)\btadi udah dibilang\b`), ""},

	// ── ENGLISH: Single words (process after multi-word) ─────────

	// EN discourse markers (sentence-start preferred)
	{regexp.MustCompile(`(?i)^\s*(basically|essentially|actually|literally|honestly|frankly|obviously|clearly|naturally|surely|certainly|definitely|truly|really|simply|anyway|anyhow|alright|okay|ok)\b[.,!\s]*`), ""},

	// EN hedging/vague qualifiers
	{regexp.MustCompile(`(?i)\b(somewhat|rather|quite|fairly)\b`), ""},

	// EN redundant intensifiers
	{regexp.MustCompile(`(?i)\b(very|extremely|absolutely|totally|completely|utterly|highly|incredibly|super)\b`), ""},

	// EN filler disfluencies
	{regexp.MustCompile(`(?i)\b(um|uh|er|ah|hmm)\b`), ""},

	// EN single-word discourse markers (mid-sentence, lower priority)
	{regexp.MustCompile(`(?i)\b(once|again)\b`), ""},

	// ── INDONESIAN: Single words (process after multi-word) ──────

	// ID discourse markers
	{regexp.MustCompile(`(?i)^\s*(oke|ok|baik|baiklah|lah|kok|sih|dong|deh|kan|ya|yah|eh|loh|lho)\b[.,!\s]*`), ""},

	// ID hedging/vague qualifiers
	{regexp.MustCompile(`(?i)\b(mungkin|sepertinya|kelihatannya|bisa dibilang|bisa dikatakan|kurang lebih|agak|lumayan|cukup|sebenarnya)\b`), ""},

	// ID intensifiers
	{regexp.MustCompile(`(?i)\b(banget|sangat|amat|sekali|super|sungguh|terlalu)\b`), ""},

	// ID casual fillers (chat/text)
	{regexp.MustCompile(`(?i)\b(anjir|anjay|wkwk|wkwkwk|haha|hehe|btw|fyi|imo|imho)\b`), ""},
}

// proseSentEnders marks sentence boundaries for the sentence scanner.
var proseSentEnders = []string{". ", ".\n", "! ", "!\n", "? ", "?\n"}

// ApplyProseFilter removes filler words/phrases from prose text.
// Supports EN + ID + mixed. Skips JSON/XML/code blocks entirely.
func ApplyProseFilter(s string) string {
	if len(s) < 100 {
		return s
	}
	if IsStructuredOutput(s) {
		return s
	}

	for _, rule := range proseRules {
		s = rule.Pattern.ReplaceAllString(s, rule.Replace)
	}

	// Collapse resulting multi-blank lines
	s = CollapseBlankLines(s)
	return strings.TrimSpace(s)
}

// IsStructuredOutput returns true if the content is JSON, XML, or code.
// Skips compression to avoid breaking structure.
func IsStructuredOutput(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return false
	}

	// JSON detection
	if (trimmed[0] == '{' || trimmed[0] == '[') &&
		(strings.HasSuffix(trimmed, "}") || strings.HasSuffix(trimmed, "]")) &&
		len(trimmed) > 20 {
		return true
	}

	// XML detection
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") && len(trimmed) > 20 {
		return true
	}

	// Code block detection
	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		return true
	}

	return false
}
