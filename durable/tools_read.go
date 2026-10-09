package durable

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of packages/durable/src/tools/read.ts at pi v1.1.0.
const readChunkSize = 64 * 1024

func executeReadTool(reader BinaryReader, input ReadToolInput, ctx chord.Context) (ToolExecutionResult, error) {
	for attempt := 0; ; attempt++ {
		before, err := reader.Info(ctx)
		if err != nil {
			return ToolExecutionResult{}, err
		}
		result, err := readToolSnapshot(reader, before, input, ctx)
		if err != nil {
			return ToolExecutionResult{}, err
		}
		after, err := reader.Info(ctx)
		if err != nil {
			return ToolExecutionResult{}, err
		}
		if after.Size > before.Size || (after.Size == before.Size && after.MtimeMs == before.MtimeMs) {
			return result, nil
		}
		if attempt == 1 {
			return ToolExecutionResult{}, fmt.Errorf("%s changed while it was read", input.Path)
		}
	}
}

func readToolSnapshot(reader BinaryReader, info *FileInfo, input ReadToolInput, ctx chord.Context) (ToolExecutionResult, error) {
	mimeType, err := detectSupportedImageMimeTypeFromReader(reader, info.Size, ctx)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	if mimeType != "" {
		return readResultError("unsupported_image",
			fmt.Sprintf("%s is an image (%s); reading images is not supported", input.Path, mimeType)), nil
	}

	startLine := 0
	if input.Offset != nil && *input.Offset-1 > 0 {
		startLine = *input.Offset - 1
	}
	start := int64(startLine)
	var scanEnd *int64
	if input.Limit != nil && *input.Limit >= 0 {
		end := addReadLineLimit(start, int64(*input.Limit))
		if end <= start {
			end = start + 1
		}
		scanEnd = &end
	}
	scan, err := reader.ScanLines(start, scanEnd, ctx)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	totalFileLines := scan.Newlines + 1
	if start >= totalFileLines {
		return ToolExecutionResult{}, fmt.Errorf("Offset %d is beyond end of file (%d lines total)", valueOr(input.Offset, 0), totalFileLines)
	}

	selectedEnd := totalFileLines
	var userLimitedLines *int
	if input.Limit != nil {
		end := addReadLineLimit(start, int64(*input.Limit))
		if end > totalFileLines {
			end = totalFileLines
		}
		limited := int(end - start)
		userLimitedLines = &limited
		selectedEnd = end
		if selectedEnd < 0 {
			selectedEnd = totalFileLines + selectedEnd
			if selectedEnd < 0 {
				selectedEnd = 0
			}
		}
	}
	selectedLineCount := selectedEnd - start
	if selectedLineCount < 0 {
		selectedLineCount = 0
	}
	if input.Limit != nil && *input.Limit < 0 && selectedLineCount > 0 {
		scan, err = reader.ScanLines(start, &selectedEnd, ctx)
		if err != nil {
			return ToolExecutionResult{}, err
		}
	}

	empty := selectedLineCount == 0
	endsWithNewline := !empty && scan.LastLineStart == scan.End && scan.LastLineStart > scan.Start
	totalLines := selectedLineCount
	if empty || scan.SelectedBytes == 0 {
		totalLines = 0
	} else if endsWithNewline {
		totalLines--
	}
	totalsBytes := int(scan.SelectedBytes)
	if empty {
		totalsBytes = 0
	}

	firstBytes, err := reader.Read(0, 3, ctx)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	bom := len(firstBytes) == 3 && firstBytes[0] == 0xef && firstBytes[1] == 0xbb && firstBytes[2] == 0xbf
	head := ""
	if !empty {
		head, err = readToolHead(reader, scan.Start, scan.End, bom, ctx)
		if err != nil {
			return ToolExecutionResult{}, err
		}
	}
	truncation := TruncateHead(head, TruncationOptions{})
	truncation.TotalLines = int(totalLines)
	truncation.TotalBytes = totalsBytes

	startLineDisplay := startLine + 1
	diagnostics := []ToolDiagnostic{}
	outputText := truncation.Content
	var details *ReadToolDetails
	if truncation.FirstLineExceedsLimit {
		firstLine := strings.SplitN(head, "\n", 2)[0]
		lineBytes := []byte(firstLine)
		end := CharacterEnd(lineBytes, DefaultMaxBytes)
		outputText = string(lineBytes[:end])
		diagnostics = append(diagnostics, ToolDiagnostic{
			Severity: DiagnosticWarn, Code: stringPointer("truncated"),
			Message: fmt.Sprintf("Line %d is %s, exceeds the %s limit; showing its first %s. Use bash: sed -n '%dp' %s | tail -c +%d",
				startLineDisplay, FormatSize(int(scan.FirstLineBytes)), FormatSize(DefaultMaxBytes), FormatSize(end), startLineDisplay, input.Path, end+1),
		})
		details = &ReadToolDetails{Truncation: &ReadTruncation{
			Truncated: truncation.Truncated, TruncatedBy: truncation.TruncatedBy,
			TotalLines: truncation.TotalLines, TotalBytes: truncation.TotalBytes,
			OutputLines: 1, OutputBytes: end, FirstLineExceedsLimit: true,
			MaxLines: truncation.MaxLines, MaxBytes: truncation.MaxBytes,
		}}
	} else if truncation.Truncated {
		endLineDisplay := startLineDisplay + truncation.OutputLines - 1
		nextOffset := endLineDisplay + 1
		limitText := ""
		if truncation.TruncatedBy != "lines" {
			limitText = " (" + FormatSize(DefaultMaxBytes) + " limit)"
		}
		diagnostics = append(diagnostics, ToolDiagnostic{
			Severity: DiagnosticInfo, Code: stringPointer("truncated"),
			Message: fmt.Sprintf("Showing lines %d-%d of %d%s. Use offset=%d to continue.",
				startLineDisplay, endLineDisplay, totalFileLines, limitText, nextOffset),
		})
		details = &ReadToolDetails{Truncation: readTruncationOf(truncation)}
	} else if userLimitedLines != nil && startLine+*userLimitedLines < int(totalFileLines) {
		remaining := int(totalFileLines) - (startLine + *userLimitedLines)
		nextOffset := startLine + *userLimitedLines + 1
		diagnostics = append(diagnostics, ToolDiagnostic{
			Severity: DiagnosticInfo,
			Message:  fmt.Sprintf("%d more lines in file. Use offset=%d to continue.", remaining, nextOffset),
		})
	}

	content := []ai.UserContent{}
	if outputText != "" {
		content = append(content, ai.TextContent{Text: outputText})
	}
	result := ToolExecutionResult{Content: content, Diagnostics: diagnostics}
	if details != nil {
		result.Details = *details
	}
	return result, nil
}

func addReadLineLimit(start, limit int64) int64 {
	end := start + limit
	if limit > 0 && end < start {
		return int64(^uint64(0) >> 1)
	}
	if limit < 0 && end > start {
		return -int64(^uint64(0)>>1) - 1
	}
	return end
}

func readToolHead(reader BinaryReader, start, end int64, bom bool, ctx chord.Context) (string, error) {
	decoder := newRangeUTF8Decoder()
	var text strings.Builder
	newlines := 0
	position := start
	if bom && start == 0 {
		position = 3
	}
	for position < end {
		length := readChunkSize
		if remaining := end - position; remaining < int64(length) {
			length = int(remaining)
		}
		chunk, err := reader.Read(position, length, ctx)
		if err != nil {
			return "", err
		}
		if len(chunk) == 0 {
			break
		}
		position += int64(len(chunk))
		decoded, err := decoder.Feed(chunk)
		if err != nil {
			return "", err
		}
		text.Write(decoded)
		newlines += bytes.Count(decoded, []byte{'\n'})
		if newlines >= DefaultMaxLines || text.Len() > DefaultMaxBytes+1 {
			return text.String(), nil
		}
	}
	decoded, err := decoder.Finish()
	if err != nil {
		return "", err
	}
	text.Write(decoded)
	return text.String(), nil
}
