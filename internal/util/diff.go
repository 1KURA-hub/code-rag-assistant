package util

import (
	"regexp"
	"strings"
)

var diffPathPattern = regexp.MustCompile(`(?m)^(?:\+\+\+|---|diff --git)\s+(?:a/|b/)?([^\s]+)`)
var identifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}`)

func ExtractDiffHints(diffText string) []string {
	seen := map[string]bool{}
	var hints []string
	for _, path := range ExtractDiffPaths(diffText) {
		addHint(&hints, seen, path)
		for _, part := range strings.FieldsFunc(path, func(r rune) bool {
			return r == '/' || r == '.' || r == '_' || r == '-'
		}) {
			if len(part) > 2 {
				addHint(&hints, seen, part)
			}
		}
	}
	for _, token := range identifierPattern.FindAllString(diffText, -1) {
		if len(token) > 2 && len(token) < 80 {
			addHint(&hints, seen, token)
		}
		if len(hints) >= 20 {
			break
		}
	}
	return hints
}

func ExtractDiffPaths(diffText string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, match := range diffPathPattern.FindAllStringSubmatch(diffText, -1) {
		if len(match) <= 1 {
			continue
		}
		path := strings.Trim(strings.TrimSpace(match[1]), "\"")
		if path == "" || path == "/dev/null" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func addHint(hints *[]string, seen map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" || seen[value] || value == "/dev/null" {
		return
	}
	seen[value] = true
	*hints = append(*hints, value)
}
