// Package stringutil provides string related utility functions.
package stringutil

import (
	"strings"
)

// Multiline handles formatting multiline text by trimming whitespace, and
// removing start and end newlines. Used in multiline string declarations.
func Multiline(text string) string {
	trimmedText := strings.TrimSpace(text)
	lines := strings.Split(trimmedText, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}
