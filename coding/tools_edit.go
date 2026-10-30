package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Port of core/tools/edit.ts.

// nodeErrorCode maps a filesystem error to Node's `error.code` string, the form
// upstream reports (`Error code: ${error.code}`).
func nodeErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		switch errno {
		case syscall.EPERM:
			return "EPERM"
		case syscall.EISDIR:
			return "EISDIR"
		case syscall.ENOTDIR:
			return "ENOTDIR"
		case syscall.ELOOP:
			return "ELOOP"
		case syscall.ENAMETOOLONG:
			return "ENAMETOOLONG"
		case syscall.EROFS:
			return "EROFS"
		case syscall.ENOSPC:
			return "ENOSPC"
		case syscall.EMFILE:
			return "EMFILE"
		case syscall.ENFILE:
			return "ENFILE"
		}
	}
	if os.IsNotExist(err) {
		return "ENOENT"
	}
	if os.IsPermission(err) {
		return "EACCES"
	}
	return ""
}

// EditFileErrorMessage is upstream's `Could not edit file: ${path}.
// ${errorMessage}.`, where errorMessage is `Error code: ${error.code}` when the
// error carries a code and String(error) otherwise.
func EditFileErrorMessage(path string, err error) string {
	if code := nodeErrorCode(err); code != "" {
		return fmt.Sprintf("Could not edit file: %s. Error code: %s.", path, code)
	}
	return fmt.Sprintf("Could not edit file: %s. %v.", path, err)
}

// EditToolDetails carries the edit's diff and patch.
type EditToolDetails struct {
	// Diff is the display-oriented diff of the changes made.
	Diff string `json:"diff"`
	// Patch is the standard unified patch.
	Patch string `json:"patch"`
	// FirstChangedLine is the first change's line number in the new file.
	FirstChangedLine *int `json:"firstChangedLine,omitempty"`
}

// EditToolDescription is upstream's edit description.
const EditToolDescription = "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes."

// EditToolSystemPromptContribution is the edit prompt contribution.
const EditToolSystemPromptContribution = "Make precise file edits with exact text replacement, including multiple disjoint edits in one call"

// EditToolSystemPromptGuidelines are the edit prompt guidelines.
var EditToolSystemPromptGuidelines = []string{
	"Use edit for precise changes (edits[].oldText must match exactly)",
	"When changing multiple separate locations in one file, use one edit call with multiple entries in edits[] instead of multiple edit calls",
	"Each edits[].oldText is matched against the original file, not after earlier edits are applied. Do not emit overlapping or nested edits. Merge nearby changes into one edit.",
	"Keep edits[].oldText as small as possible while still being unique in the file. Do not pad with large unchanged regions.",
}

// The nested edit schema keeps TypeBox's object and property insertion order.
var editSchema = mustMarshalJSON(json.RawMessage(`{
	"type": "object",
	"required": ["path", "edits"],
	"properties": {
		"path": {"type": "string", "description": "Path to the file to edit (relative or absolute)"},
		"edits": {
			"type": "array",
			"items": {
				"type": "object",
				"required": ["oldText", "newText"],
				"properties": {
					"oldText": {"type": "string", "description": "Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."},
					"newText": {"type": "string", "description": "Replacement text for this targeted edit."}
				}
			},
			"description": "One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead."
		}
	}
}`))

// PrepareEditArguments ports the edit tool's prepareArguments shim: JSON-
// string edits arrays, single-edit objects, and the legacy oldText/newText
// shape all normalize to the edits[] array. The prepared object keeps the
// caller's key order (upstream spreads the input and overwrites edits in
// place), which RPC tool_execution_start args expose.
func PrepareEditArguments(input json.RawMessage) json.RawMessage {
	keys, args, err := readJSONObjectOrdered(bytes.TrimSpace(input))
	if err != nil {
		return input
	}
	order := append([]string{}, keys...)
	setValue := func(key string, value json.RawMessage) {
		if _, ok := args[key]; !ok {
			order = append(order, key)
		}
		args[key] = value
	}
	removeValue := func(key string) {
		if _, ok := args[key]; !ok {
			return
		}
		delete(args, key)
		for index, candidate := range order {
			if candidate == key {
				order = append(order[:index], order[index+1:]...)
				break
			}
		}
	}
	editShape := struct {
		OldText *string `json:"oldText"`
		NewText *string `json:"newText"`
	}{}

	// Some models (Opus 4.6, GLM-5.1) send edits as a JSON string instead of
	// an array; others send a single edit object instead of a one-element
	// array.
	if raw, ok := args["edits"]; ok {
		var asString string
		if jsonUnmarshalStrictTool(raw, &asString) == nil {
			var parsed []json.RawMessage
			if err := jsonUnmarshalStrictTool(json.RawMessage(asString), &parsed); err != nil {
				if jsonUnmarshalStrictTool(json.RawMessage(asString), &editShape) == nil && editShape.OldText != nil && editShape.NewText != nil {
					setValue("edits", mustMarshalJSON([]json.RawMessage{json.RawMessage(asString)}))
				}
			} else {
				setValue("edits", json.RawMessage(asString))
			}
		} else if jsonUnmarshalStrictTool(raw, &editShape) == nil && editShape.OldText != nil && editShape.NewText != nil {
			// Single edit object?
			setValue("edits", mustMarshalJSON([]json.RawMessage{raw}))
		}
	}

	// Legacy flat shape: oldText/newText at the top level.
	legacy := struct {
		OldText *string `json:"oldText"`
		NewText *string `json:"newText"`
	}{}
	if jsonUnmarshalStrictTool(input, &legacy) != nil || legacy.OldText == nil || legacy.NewText == nil {
		return marshalOrderedObject(order, args)
	}
	edits := []json.RawMessage{}
	if raw, ok := args["edits"]; ok {
		var list []json.RawMessage
		if jsonUnmarshalStrictTool(raw, &list) == nil {
			edits = append(edits, list...)
		}
	}
	// Upstream builds { oldText, newText } in that order.
	editJSON := "{" + string(mustMarshalJSON("oldText")) + ":" + string(mustMarshalJSON(*legacy.OldText)) + "," +
		string(mustMarshalJSON("newText")) + ":" + string(mustMarshalJSON(*legacy.NewText)) + "}"
	edits = append(edits, json.RawMessage(editJSON))
	setValue("edits", mustMarshalJSON(edits))
	removeValue("oldText")
	removeValue("newText")
	return marshalOrderedObject(order, args)
}

// marshalOrderedObject serializes an ordered key list with raw values.
func marshalOrderedObject(order []string, values map[string]json.RawMessage) json.RawMessage {
	var out strings.Builder
	out.WriteByte('{')
	written := 0
	for _, key := range order {
		value, ok := values[key]
		if !ok {
			continue
		}
		if written > 0 {
			out.WriteByte(',')
		}
		written++
		out.Write(mustMarshalJSON(key))
		out.WriteByte(':')
		out.Write(bytes.TrimSpace(value))
	}
	out.WriteByte('}')
	return json.RawMessage(out.String())
}

// CreateEditTool builds the edit tool.
func CreateEditTool(cwd string) agent.AgentTool {
	return agent.AgentTool{
		Name:                "edit",
		Description:         EditToolDescription,
		Parameters:          editSchema,
		Label:               "edit",
		ConstrainedSampling: preferStrictJSONSchema,
		PrepareArguments: func(args json.RawMessage) json.RawMessage {
			return PrepareEditArguments(args)
		},
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				Path  string `json:"path"`
				Edits []Edit `json:"edits"`
			}
			if err := jsonUnmarshalStrictTool(params, &input); err != nil {
				return agent.AgentToolResult{}, err
			}
			if len(input.Edits) == 0 {
				return agent.AgentToolResult{}, fmt.Errorf("Edit tool input is invalid. edits must contain at least one replacement.")
			}
			absolutePath := ResolveToCwd(input.Path, cwd)

			var resultValue agent.AgentToolResult
			var resultErr error
			err := WithFileMutationQueue(absolutePath, func() error {
				if ctxErrOf(ctx) != nil {
					resultErr = fmt.Errorf("Operation aborted")
					return resultErr
				}

				// Check the file exists (access check).
				if _, err := os.Stat(absolutePath); err != nil {
					resultErr = fmt.Errorf("%s", EditFileErrorMessage(input.Path, err))
					return resultErr
				}
				if ctxErrOf(ctx) != nil {
					resultErr = fmt.Errorf("Operation aborted")
					return resultErr
				}

				data, err := os.ReadFile(absolutePath)
				if err != nil {
					resultErr = err
					return err
				}
				if ctxErrOf(ctx) != nil {
					resultErr = fmt.Errorf("Operation aborted")
					return resultErr
				}
				rawContent := DecodeUTF8WithReplacement(data)
				// Strip the BOM before matching: the model will not include an
				// invisible BOM in oldText.
				bom, content := SplitBom(rawContent)
				originalEnding := DetectLineEnding(content)
				normalizedContent := NormalizeToLF(content)
				applied, err := ApplyEditsToNormalizedContent(normalizedContent, input.Edits, input.Path)
				if err != nil {
					resultErr = err
					return err
				}
				if ctxErrOf(ctx) != nil {
					resultErr = fmt.Errorf("Operation aborted")
					return resultErr
				}

				finalContent := bom + RestoreLineEndings(applied.NewContent, originalEnding)
				if err := os.WriteFile(absolutePath, []byte(finalContent), 0o644); err != nil {
					resultErr = err
					return err
				}
				if ctxErrOf(ctx) != nil {
					resultErr = fmt.Errorf("Operation aborted")
					return resultErr
				}

				diff, firstChangedLine, _ := GenerateDiffString(applied.BaseContent, applied.NewContent, 4)
				patch := GenerateUnifiedPatch(input.Path, applied.BaseContent, applied.NewContent, 4)
				details := EditToolDetails{Diff: diff, Patch: patch}
				if firstChangedLine >= 0 {
					details.FirstChangedLine = &firstChangedLine
				}
				detailsJSON, _ := ai.MarshalJSON(details)
				resultValue = agent.AgentToolResult{
					Content: []ai.Content{ai.TextContent{Text: fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(input.Edits), input.Path)}},
					Details: detailsJSON,
				}
				return nil
			})
			if resultErr != nil {
				return agent.AgentToolResult{}, resultErr
			}
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			return resultValue, nil
		},
	}
}
