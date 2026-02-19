package tool

import (
	"strings"
	"unicode"
)

type fuzzyMatchResult struct {
	Found                 bool
	Index                 int
	MatchLength           int
	ContentForReplacement string
}

func detectLineEnding(content string) string {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	if lfIdx == -1 || crlfIdx == -1 {
		return "\n"
	}
	if crlfIdx < lfIdx {
		return "\r\n"
	}
	return "\n"
}

func normalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func restoreLineEndings(text string, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

func normalizeForFuzzyMatch(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	normalized := strings.Join(lines, "\n")

	replacer := strings.NewReplacer(
		"\u2018", "'",
		"\u2019", "'",
		"\u201A", "'",
		"\u201B", "'",
		"\u201C", "\"",
		"\u201D", "\"",
		"\u201E", "\"",
		"\u201F", "\"",
		"\u2010", "-",
		"\u2011", "-",
		"\u2012", "-",
		"\u2013", "-",
		"\u2014", "-",
		"\u2015", "-",
		"\u2212", "-",
		"\u00A0", " ",
		"\u2002", " ",
		"\u2003", " ",
		"\u2004", " ",
		"\u2005", " ",
		"\u2006", " ",
		"\u2007", " ",
		"\u2008", " ",
		"\u2009", " ",
		"\u200A", " ",
		"\u202F", " ",
		"\u205F", " ",
		"\u3000", " ",
	)
	return replacer.Replace(normalized)
}

func fuzzyFindText(content string, oldText string) fuzzyMatchResult {
	if idx := strings.Index(content, oldText); idx >= 0 {
		return fuzzyMatchResult{
			Found:                 true,
			Index:                 idx,
			MatchLength:           len(oldText),
			ContentForReplacement: content,
		}
	}

	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	idx := strings.Index(fuzzyContent, fuzzyOldText)
	if idx < 0 {
		return fuzzyMatchResult{
			Found:                 false,
			Index:                 -1,
			MatchLength:           0,
			ContentForReplacement: content,
		}
	}

	return fuzzyMatchResult{
		Found:                 true,
		Index:                 idx,
		MatchLength:           len(fuzzyOldText),
		ContentForReplacement: fuzzyContent,
	}
}

func stripBOM(content string) (bom string, text string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", strings.TrimPrefix(content, "\uFEFF")
	}
	return "", content
}
