package coding

import (
	"slices"
	"strings"
)

// Port of the CLI's diagnostic reporting: upstream main.ts prints the parse
// diagnostics straight after parseArgs and treats any error among them as
// fatal, and reportDiagnostics() prints the runtime ones with the same shape.

const (
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiDim    = "\x1b[2m"
	// chalk closes a foreground color with 39 and an intensity attribute with
	// 22; diagnostics are byte-identical to the reference's pty output (D381).
	ansiFgReset  = "\x1b[39m"
	ansiDimReset = "\x1b[22m"
)

// FormatCLIDiagnostic renders one diagnostic the way upstream's CLI does:
// errors and warnings carry a matching colour and an "Error: "/"Warning: "
// prefix, and any other type is dimmed with no prefix.
func FormatCLIDiagnostic(d CLIDiagnostic) string {
	color, prefix := ansiDim, ""
	switch d.Type {
	case "error":
		color, prefix = ansiRed, "Error: "
	case "warning":
		color, prefix = ansiYellow, "Warning: "
	}
	if !CLIColorsEnabled() {
		return prefix + d.Message
	}
	closer := ansiFgReset
	if color == ansiDim {
		closer = ansiDimReset
	}
	return color + prefix + d.Message + closer
}

// HasErrorDiagnostics reports whether any diagnostic is an error, which is what
// upstream treats as a fatal argument error.
func HasErrorDiagnostics(diagnostics []CLIDiagnostic) bool {
	for _, d := range diagnostics {
		if d.Type == "error" {
			return true
		}
	}
	return false
}

// UnknownFlagDiagnostics reports the flags ParseArgs could not recognize
// (upstream agent-session-services' unknownFlags): an extension may claim a
// long flag there, and whatever is left over is a fatal "Unknown option".
// Extensions are out of scope (D41), so every unrecognized flag is unknown
// here and a misspelled option fails the run instead of being ignored.
// Flags come back in argv order, one diagnostic for all of them, singular or
// plural like the reference.
func UnknownFlagDiagnostics(args *Args) []CLIDiagnostic {
	if args == nil {
		return nil
	}
	names := make([]string, 0, len(args.UnknownFlags))
	for _, name := range args.UnknownFlagOrder {
		names = append(names, "--"+name)
	}
	if len(names) == 0 {
		for name := range args.UnknownFlags {
			names = append(names, "--"+name)
		}
		slices.Sort(names)
	}
	if len(names) == 0 {
		return nil
	}
	label := "Unknown option"
	if len(names) > 1 {
		label = "Unknown options"
	}
	return []CLIDiagnostic{{Type: "error", Message: label + ": " + strings.Join(names, ", ")}}
}

// FormatCLIError renders an error without the "Error: " prefix, for callers
// whose text is already complete (upstream's chalk.red lines in main.ts).
func FormatCLIError(text string) string {
	if !CLIColorsEnabled() {
		return text
	}
	return ansiRed + text + ansiFgReset
}

// FormatCLIWarning renders a bare warning line the way upstream's chalk.yellow
// calls do: colored only where the CLI supports color.
func FormatCLIWarning(text string) string {
	if !CLIColorsEnabled() {
		return text
	}
	return ansiYellow + text + ansiFgReset
}
