package mcp

import "time"

// maxJavascriptTimeoutMs is the largest Node/Bun setTimeout delay accepted
// without clamping it to 1ms.
const maxJavascriptTimeoutMs = 1<<31 - 1

func javascriptTimerDuration(ms int64) time.Duration {
	if ms < 1 || ms > maxJavascriptTimeoutMs {
		ms = 1
	}
	return time.Duration(ms) * time.Millisecond
}
