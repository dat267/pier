package durable

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/coding"
)

// Port of the durable edit tool (tools/edit.ts).

// EditToolInput is one edit tool call.
type EditToolInput struct {
	Path  string         `json:"path"`
	Edits []EditToolEdit `json:"edits"`
}

// EditToolEdit is one targeted replacement.
type EditToolEdit struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// EditToolDetails is the edit tool's structured details.
type EditToolDetails struct {
	Diff             string `json:"diff"`
	Patch            string `json:"patch"`
	FirstChangedLine *int   `json:"firstChangedLine,omitempty"`
}

// PrepareEditArguments repairs the shapes models commonly send: `edits` as a
// JSON string or a single edit object, and a top-level oldText/newText pair.
func PrepareEditArguments(args chord.JsonValue) (chord.JsonValue, error) {
	object, ok := args.(map[string]any)
	if !ok {
		return args, nil
	}
	repaired := map[string]any{}
	for key, value := range object {
		repaired[key] = value
	}
	if text, ok := repaired["edits"].(string); ok {
		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			if list, ok := parsed.([]any); ok {
				repaired["edits"] = list
			} else if isSingleEdit(parsed) {
				repaired["edits"] = []any{parsed}
			}
		}
	} else if isSingleEdit(repaired["edits"]) {
		repaired["edits"] = []any{repaired["edits"]}
	}
	legacyOld, hasOld := repaired["oldText"].(string)
	legacyNew, hasNew := repaired["newText"].(string)
	if !hasOld || !hasNew {
		return repaired, nil
	}
	edits, _ := repaired["edits"].([]any)
	edits = append(edits, map[string]any{"oldText": legacyOld, "newText": legacyNew})
	delete(repaired, "oldText")
	delete(repaired, "newText")
	repaired["edits"] = edits
	return repaired, nil
}

func isSingleEdit(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, hasOld := object["oldText"].(string)
	_, hasNew := object["newText"].(string)
	return hasOld && hasNew
}

func editAccessError(path string, err error) error {
	var fileError *FileError
	if errors.As(err, &fileError) {
		return fmt.Errorf("Could not edit file: %s. Error code: %s.", path, fileError.Code)
	}
	return fmt.Errorf("Could not edit file: %s. %v", path, err)
}

// CreateEditTool builds the edit tool: exact-text replacement of one file.
func CreateEditTool() ToolRegistration {
	return ToolRegistration{
		Tool: ai.Tool{
			Name:        "edit",
			Description: "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to edit (relative or absolute)"},"edits":{"type":"array","description":"One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead.","items":{"type":"object","properties":{"oldText":{"type":"string","description":"Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."},"newText":{"type":"string","description":"Replacement text for this targeted edit."}},"required":["oldText","newText"]}},"required":["path","edits"]}`),
		},
		PrepareArguments: PrepareEditArguments,
		Execute: func(args chord.JsonValue, api ToolExecutionApi, ctx chord.Context) (ToolExecutionResult, error) {
			var input EditToolInput
			if err := decodeArgs(args, &input); err != nil {
				return ToolExecutionResult{}, err
			}
			if len(input.Edits) == 0 {
				return ToolExecutionResult{}, fmt.Errorf("Edit tool input is invalid. edits must contain at least one replacement.")
			}
			env, err := RequireEnv(api)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			absolutePath, err := ResolveToolPath(ctx, env, input.Path)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			edits := make([]coding.Edit, 0, len(input.Edits))
			for _, edit := range input.Edits {
				edits = append(edits, coding.Edit{OldText: edit.OldText, NewText: edit.NewText})
			}
			return WithFileMutationQueue(ctx, env, absolutePath, func() (ToolExecutionResult, error) {
				if ctx != nil && ctx.Err() != nil {
					return ToolExecutionResult{}, fmt.Errorf("Operation aborted")
				}
				info, err := env.FileInfo(absolutePath, ctx)
				if err != nil {
					return ToolExecutionResult{}, editAccessError(input.Path, err)
				}
				if info.Kind != FileKindFile && info.Kind != FileKindSymlink {
					return ToolExecutionResult{}, fmt.Errorf("Could not edit file: %s. Path is not a file.", input.Path)
				}
				content, err := env.ReadTextFile(absolutePath, ctx)
				if err != nil {
					return ToolExecutionResult{}, editAccessError(input.Path, err)
				}
				if ctx != nil && ctx.Err() != nil {
					return ToolExecutionResult{}, fmt.Errorf("Operation aborted")
				}
				bom, text := coding.SplitBom(content)
				originalEnding := coding.DetectLineEnding(text)
				normalized := coding.NormalizeToLF(text)
				applied, err := coding.ApplyEditsToNormalizedContent(normalized, edits, input.Path)
				if err != nil {
					return ToolExecutionResult{}, err
				}
				if ctx != nil && ctx.Err() != nil {
					return ToolExecutionResult{}, fmt.Errorf("Operation aborted")
				}
				finalContent := bom + coding.RestoreLineEndings(applied.NewContent, originalEnding)
				if err := env.WriteFile(absolutePath, []byte(finalContent), ctx); err != nil {
					return ToolExecutionResult{}, editAccessError(input.Path, err)
				}
				if ctx != nil && ctx.Err() != nil {
					return ToolExecutionResult{}, fmt.Errorf("Operation aborted")
				}
				diff, firstChanged, _ := coding.GenerateDiffString(applied.BaseContent, applied.NewContent, 0)
				details := EditToolDetails{Diff: diff, Patch: coding.GenerateUnifiedPatch(input.Path, applied.BaseContent, applied.NewContent, 0)}
				if firstChanged > 0 {
					line := firstChanged
					details.FirstChangedLine = &line
				}
				return ToolExecutionResult{
					Content: []ai.UserContent{ai.TextContent{
						Text: fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(input.Edits), input.Path),
					}},
					Details: details,
				}, nil
			})
		},
	}
}
