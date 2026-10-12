package tui

// jsLength counts UTF-16 code units, matching JavaScript's string.length.
// Upstream measures paste size and similar thresholds with `.length`, where an
// astral character counts as two.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
