package tool

import (
	"fmt"
	"strings"
)

const (
	defaultMaxLines = 2000
	defaultMaxBytes = 50 * 1024
	grepMaxLineLen  = 500
)

type truncationResult struct {
	Content               string `json:"content"`
	Truncated             bool   `json:"truncated"`
	TruncatedBy           string `json:"truncated_by"`
	TotalLines            int    `json:"total_lines"`
	TotalBytes            int    `json:"total_bytes"`
	OutputLines           int    `json:"output_lines"`
	OutputBytes           int    `json:"output_bytes"`
	LastLinePartial       bool   `json:"last_line_partial"`
	FirstLineExceedsLimit bool   `json:"first_line_exceeds_limit"`
}

type truncationOptions struct {
	MaxLines int
	MaxBytes int
}

func formatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024.0)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024.0*1024.0))
	}
}

func truncateHead(content string, options truncationOptions) truncationResult {
	maxLines := options.MaxLines
	if maxLines <= 0 {
		maxLines = defaultMaxLines
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	totalBytes := len([]byte(content))
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncationResult{
			Content:               content,
			Truncated:             false,
			TruncatedBy:           "",
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           totalLines,
			OutputBytes:           totalBytes,
			LastLinePartial:       false,
			FirstLineExceedsLimit: false,
		}
	}

	firstLineBytes := len([]byte(lines[0]))
	if firstLineBytes > maxBytes {
		return truncationResult{
			Content:               "",
			Truncated:             true,
			TruncatedBy:           "bytes",
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           0,
			OutputBytes:           0,
			LastLinePartial:       false,
			FirstLineExceedsLimit: true,
		}
	}

	outputLines := make([]string, 0, min(totalLines, maxLines))
	outputBytesCount := 0
	truncatedBy := "lines"

	for i := range lines {
		if i >= maxLines {
			break
		}

		line := lines[i]
		lineBytes := len([]byte(line))
		if i > 0 {
			lineBytes++
		}

		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}

		outputLines = append(outputLines, line)
		outputBytesCount += lineBytes
	}

	if len(outputLines) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = "lines"
	}

	outputContent := strings.Join(outputLines, "\n")
	finalOutputBytes := len([]byte(outputContent))

	return truncationResult{
		Content:               outputContent,
		Truncated:             true,
		TruncatedBy:           truncatedBy,
		TotalLines:            totalLines,
		TotalBytes:            totalBytes,
		OutputLines:           len(outputLines),
		OutputBytes:           finalOutputBytes,
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
	}
}

func truncateTail(content string, options truncationOptions) truncationResult {
	maxLines := options.MaxLines
	if maxLines <= 0 {
		maxLines = defaultMaxLines
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	totalBytes := len([]byte(content))
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncationResult{
			Content:               content,
			Truncated:             false,
			TruncatedBy:           "",
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           totalLines,
			OutputBytes:           totalBytes,
			LastLinePartial:       false,
			FirstLineExceedsLimit: false,
		}
	}

	outputLines := make([]string, 0, min(totalLines, maxLines))
	outputBytesCount := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(outputLines) < maxLines; i-- {
		line := lines[i]
		lineBytes := len([]byte(line))
		if len(outputLines) > 0 {
			lineBytes++
		}

		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(outputLines) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(line, maxBytes)
				outputLines = append(outputLines, truncatedLine)
				outputBytesCount = len([]byte(truncatedLine))
				lastLinePartial = true
			}
			break
		}

		outputLines = append(outputLines, line)
		outputBytesCount += lineBytes
	}

	reverseStrings(outputLines)

	if len(outputLines) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = "lines"
	}

	outputContent := strings.Join(outputLines, "\n")
	finalOutputBytes := len([]byte(outputContent))

	return truncationResult{
		Content:               outputContent,
		Truncated:             true,
		TruncatedBy:           truncatedBy,
		TotalLines:            totalLines,
		TotalBytes:            totalBytes,
		OutputLines:           len(outputLines),
		OutputBytes:           finalOutputBytes,
		LastLinePartial:       lastLinePartial,
		FirstLineExceedsLimit: false,
	}
}

func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	raw := []byte(s)
	if len(raw) <= maxBytes {
		return s
	}

	start := len(raw) - maxBytes
	for start < len(raw) && (raw[start]&0xC0) == 0x80 {
		start++
	}
	return string(raw[start:])
}

func reverseStrings(items []string) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}

func truncateLine(line string, maxChars int) (text string, wasTruncated bool) {
	limit := maxChars
	if limit <= 0 {
		limit = grepMaxLineLen
	}
	runes := []rune(line)
	if len(runes) <= limit {
		return line, false
	}
	return string(runes[:limit]) + "... [truncated]", true
}
