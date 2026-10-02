package durable

// Port of the harness types the entry helpers reference (harness/types.ts):
// tool diagnostics and the compaction reason.

// ToolDiagnostic is one structured tool diagnostic (upstream ToolDiagnostic).
type ToolDiagnostic struct {
	// Severity is "info", "warn" or "error".
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	Code     *string `json:"code,omitempty"`
}

// Diagnostic severities.
const (
	DiagnosticInfo  = "info"
	DiagnosticWarn  = "warn"
	DiagnosticError = "error"
)

// CompactionReason is why a compaction ran (upstream CompactionReason).
type CompactionReason = string

// Compaction reasons.
const (
	CompactionManual    CompactionReason = "manual"
	CompactionThreshold CompactionReason = "threshold"
	CompactionOverflow  CompactionReason = "overflow"
)
