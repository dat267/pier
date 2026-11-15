package coding

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) packages/coding-agent/src/cli/args.ts
// parseArgs, run by testdata/cliargs/generate.mjs over a fixed argv corpus.
// Only the fields pier ports are compared, and only when set, so JS undefined
// and an absent Go nil or empty slice agree.
func TestParseArgsMatchesReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/cliargs/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Argv   []string       `json:"argv"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty fixture")
	}
	for _, fixture := range fixtures {
		got := projectCLIArgs(ParseArgs(fixture.Argv))
		if !reflect.DeepEqual(got, fixture.Result) {
			t.Errorf("ParseArgs(%q) =\n %#v\nwant\n %#v", fixture.Argv, got, fixture.Result)
		}
	}
}

// projectCLIArgs renders the parsed arguments in the fixture's shape.
func projectCLIArgs(args *Args) map[string]any {
	out := map[string]any{}
	for key, value := range map[string]*string{
		"provider":     args.Provider,
		"model":        args.Model,
		"apiKey":       args.APIKey,
		"systemPrompt": args.SystemPrompt,
		"name":         args.Name,
		"session":      args.Session,
		"sessionId":    args.SessionID,
		"fork":         args.Fork,
		"sessionDir":   args.SessionDir,
		"useTheme":     args.UseTheme,
		"tuiMode":      args.TuiMode,
		"export":       args.Export,
	} {
		if value != nil {
			out[key] = *value
		}
	}
	if args.Thinking != nil {
		out["thinking"] = string(*args.Thinking)
	}
	if args.Mode != "" {
		out["mode"] = args.Mode
	}
	for key, value := range map[string]bool{
		"continue":          args.Continue,
		"resume":            args.Resume,
		"help":              args.Help,
		"version":           args.Version,
		"noSession":         args.NoSession,
		"noTools":           args.NoTools,
		"noBuiltinTools":    args.NoBuiltinTools,
		"noExtensions":      args.NoExtensions,
		"noMcp":             args.NoMCP,
		"print":             args.Print,
		"noSkills":          args.NoSkills,
		"noPromptTemplates": args.NoPromptTemplates,
		"noThemes":          args.NoThemes,
		"noContextFiles":    args.NoContextFiles,
		"offline":           args.Offline,
		"verbose":           args.Verbose,
	} {
		if value {
			out[key] = true
		}
	}
	for key, values := range map[string][]string{
		"appendSystemPrompt": args.AppendSystemPrompt,
		"models":             args.Models,
		"tools":              args.Tools,
		"excludeTools":       args.ExcludeTools,
		"extensions":         args.Extensions,
		"skills":             args.Skills,
		"promptTemplates":    args.PromptTemplates,
		"themes":             args.Themes,
		"messages":           args.Messages,
		"fileArgs":           args.FileArgs,
	} {
		if len(values) == 0 {
			continue
		}
		items := make([]any, len(values))
		for index, value := range values {
			items[index] = value
		}
		out[key] = items
	}
	if args.ListModelsAll {
		out["listModels"] = true
	} else if args.ListModels != nil {
		out["listModels"] = *args.ListModels
	}
	if args.ProjectTrustOverride != nil {
		out["projectTrustOverride"] = *args.ProjectTrustOverride
	}
	if len(args.UnknownFlags) > 0 {
		flags := make(map[string]any, len(args.UnknownFlags))
		for key, value := range args.UnknownFlags {
			flags[key] = value
		}
		out["unknownFlags"] = flags
	}
	if len(args.Diagnostics) > 0 {
		diagnostics := make([]any, 0, len(args.Diagnostics))
		for _, diagnostic := range args.Diagnostics {
			// D472: pier adds one warning per -e value; the reference accepts the
			// flag silently. The documented difference is dropped so the rest of
			// the parse stays under the oracle.
			if strings.HasPrefix(diagnostic.Message, "Ignoring ") && strings.HasSuffix(diagnostic.Message, "this build loads no extensions.") {
				continue
			}
			diagnostics = append(diagnostics, diagnostic.Type+":"+diagnostic.Message)
		}
		if len(diagnostics) > 0 {
			out["diagnostics"] = diagnostics
		}
	}
	return out
}
