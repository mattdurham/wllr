package main

// SKILL.md frontmatter parsing. Untagged so it compiles on the host for unit
// tests; it depends only on the standard library, so it needs no host stubs.

import "strings"

// parseFrontmatter parses a SKILL.md file and returns the frontmatter fields
// as a map[string]string plus the body text (everything after the closing ---).
// If the file does not start with ---, returns nil map and the full content as body.
func parseFrontmatter(content string) (map[string]string, string) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, strings.TrimSpace(content)
	}

	// Find the closing ---
	rest := content[4:] // skip opening ---\n
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// Check for trailing --- at end of file.
		trimmed := strings.TrimRight(rest, "\n")
		if strings.HasSuffix(trimmed, "\n---") {
			idx := strings.LastIndex(rest, "\n---")
			fm := parseFrontmatterFields(rest[:idx])
			return fm, ""
		}
		return nil, strings.TrimSpace(content)
	}

	fmText := rest[:end]
	body := strings.TrimSpace(rest[end+5:]) // skip \n---\n
	fm := parseFrontmatterFields(fmText)
	return fm, body
}

// parseFrontmatterFields parses simple "key: value" lines into a map.
// Quoted string values have surrounding quotes stripped.
func parseFrontmatterFields(text string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes from string values.
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		fields[key] = val
	}
	return fields
}
