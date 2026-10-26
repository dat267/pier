package coding

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// pi v1.4.0 (4a080252), packages/coding-agent/src/cli/args.ts parseArgs.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/cli-args/generate.mjs ./pi > coding/testdata/cli-args/golden.json
func TestParseArgsMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/cli-args/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Argv     []string       `json:"argv"`
		Expected map[string]any `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty CLI args corpus")
	}
	for _, fixture := range fixtures {
		encoded, _ := json.Marshal(fixture.Argv)
		t.Run(string(encoded), func(t *testing.T) {
			got := cliArgsProjection(ParseArgs(fixture.Argv))
			encodedGot, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			encodedWant, err := json.Marshal(fixture.Expected)
			if err != nil {
				t.Fatal(err)
			}
			if string(encodedGot) != string(encodedWant) {
				t.Fatalf("args %v differ\n got: %s\nwant: %s", fixture.Argv, encodedGot, encodedWant)
			}
		})
	}
}

func cliArgsProjection(args *Args) map[string]any {
	pointer := func(value *string) any {
		if value == nil {
			return nil
		}
		return *value
	}
	// Upstream initializes these to empty arrays, and an unset mode is
	// undefined rather than an empty string.
	slice := func(values []string) any {
		if values == nil {
			return []string{}
		}
		return values
	}
	mode := any(nil)
	if args.Mode != "" {
		mode = string(args.Mode)
	}
	thinking := any(nil)
	if args.Thinking != nil {
		thinking = string(*args.Thinking)
	}
	unknown := make([][2]any, 0, len(args.UnknownFlags))
	names := make([]string, 0, len(args.UnknownFlags))
	for name := range args.UnknownFlags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		unknown = append(unknown, [2]any{name, args.UnknownFlags[name]})
	}
	diagnostics := make([]any, 0, len(args.Diagnostics))
	for _, diagnostic := range args.Diagnostics {
		diagnostics = append(diagnostics, map[string]any{"type": diagnostic.Type, "message": diagnostic.Message})
	}
	listModels := pointer(args.ListModels)
	return map[string]any{
		"provider": pointer(args.Provider), "model": pointer(args.Model), "apiKey": pointer(args.APIKey),
		"systemPrompt": pointer(args.SystemPrompt), "appendSystemPrompt": slice(args.AppendSystemPrompt),
		"thinking": thinking, "continue": args.Continue, "resume": args.Resume,
		"help": args.Help, "version": args.Version, "mode": mode, "name": pointer(args.Name),
		"noSession": args.NoSession, "session": pointer(args.Session), "sessionId": pointer(args.SessionID),
		"fork": pointer(args.Fork), "sessionDir": pointer(args.SessionDir), "models": slice(args.Models),
		"tools": slice(args.Tools), "excludeTools": slice(args.ExcludeTools), "noTools": args.NoTools,
		"noBuiltinTools": args.NoBuiltinTools, "noExtensions": args.NoExtensions, "noMcp": args.NoMCP,
		"print": args.Print, "export": pointer(args.Export), "noSkills": args.NoSkills, "skills": slice(args.Skills),
		"promptTemplates": slice(args.PromptTemplates), "noPromptTemplates": args.NoPromptTemplates,
		"themes": slice(args.Themes), "useTheme": pointer(args.UseTheme), "noThemes": args.NoThemes,
		"noContextFiles": args.NoContextFiles, "listModels": listModels, "offline": args.Offline,
		"tuiMode": pointer(args.TuiMode), "verbose": args.Verbose,
		"projectTrustOverride": args.ProjectTrustOverride,
		"messages":             slice(args.Messages), "fileArgs": slice(args.FileArgs),
		"unknownFlags": unknown, "diagnostics": diagnostics,
	}
}
