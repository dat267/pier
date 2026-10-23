package tui

import "strings"

// D263: italic is an outer style for expanded thinking, including Markdown
// elements that bypass DefaultTextStyle. Keep other attributes and OSC payloads
// intact, restore italic after SGR resets, and close it at the line boundary.
func forceMarkdownItalic(line string) string {
	if line == "" || IsImageLine(line) {
		return line
	}
	var result strings.Builder
	result.WriteString("\x1b[3m")
	tracker := ansiCodeTracker{italic: true}
	for index := 0; index < len(line); {
		if code, length := ExtractANSICode(line, index); length > 0 {
			result.WriteString(code)
			if strings.HasPrefix(code, "\x1b[") && strings.HasSuffix(code, "m") {
				tracker.process(code)
				parameters := code[2 : len(code)-1]
				// Empty SGR parameters can reset attributes too. Reasserting
				// italic is harmless when an empty field belongs to a color.
				emptyParameter := strings.HasPrefix(parameters, ";") || strings.HasSuffix(parameters, ";") || strings.Contains(parameters, ";;")
				if !tracker.italic || emptyParameter {
					result.WriteString("\x1b[3m")
					tracker.italic = true
				}
			}
			index += length
			continue
		}
		end := index + 1
		for end < len(line) && line[end] != '\x1b' {
			end++
		}
		result.WriteString(line[index:end])
		index = end
	}
	result.WriteString("\x1b[23m")
	return result.String()
}
