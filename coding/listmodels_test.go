package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

func TestFormatTokenCount(t *testing.T) {
	cases := []struct {
		count int64
		want  string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1K"},
		{1500, "1.5K"},
		{8192, "8.2K"},
		{200000, "200K"},
		// Truncating at the boundary rounds up rather than rolling over, which is
		// what upstream's toFixed(1) does: 999.999 -> "1000.0".
		{999999, "1000.0K"},
		{1000000, "1M"},
		{1500000, "1.5M"},
		{2000000, "2M"},
		// toFixed(1) rounds half away from zero.
		{1250, "1.3K"},
		{1250000, "1.3M"},
	}
	for _, testCase := range cases {
		if got := FormatTokenCount(testCase.count); got != testCase.want {
			t.Errorf("FormatTokenCount(%d) = %q, want %q", testCase.count, got, testCase.want)
		}
	}
}

func listModel(provider, id string, contextWindow, maxTokens int64, reasoning bool, input ...string) *ai.Model {
	return &ai.Model{
		Provider: provider, ID: id, ContextWindow: contextWindow, MaxTokens: maxTokens,
		Reasoning: reasoning, Input: input,
	}
}

// The table is upstream cli/list-models.ts: provider/model/context/max-out/
// thinking/images, two-space columns sized to the widest cell, sorted by
// provider then model id.
func TestListModelsTextTable(t *testing.T) {
	models := []*ai.Model{
		listModel("openai", "gpt-y", 128000, 4096, false, "text"),
		listModel("anthropic", "claude-x", 200000, 8192, true, "text", "image"),
	}

	got := ListModelsText(models, "")
	// Every column is padded, including the last, so rows carry trailing spaces —
	// that is upstream's join of six padEnd calls, reproduced rather than tidied.
	want := strings.Join([]string{
		"provider   model     context  max-out  thinking  images",
		"anthropic  claude-x  200K     8.2K     yes       yes   ",
		"openai     gpt-y     128K     4.1K     no        no    ",
		"",
	}, "\n")
	if got != want {
		t.Errorf("table:\n got %q\nwant %q", got, want)
	}
}

func TestListModelsTextSortsByProviderThenID(t *testing.T) {
	models := []*ai.Model{
		listModel("openai", "z", 1000, 1000, false, "text"),
		listModel("anthropic", "b", 1000, 1000, false, "text"),
		listModel("anthropic", "a", 1000, 1000, false, "text"),
	}
	got := ListModelsText(models, "")
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4 (header + 3 rows):\n%s", len(lines), got)
	}
	// Column 0 is the provider, and within a provider the ids ascend.
	if !strings.HasPrefix(lines[1], "anthropic  a") || !strings.HasPrefix(lines[2], "anthropic  b") {
		t.Errorf("rows are not sorted by id:\n%s", got)
	}
	if !strings.HasPrefix(lines[3], "openai") {
		t.Errorf("rows are not sorted by provider:\n%s", got)
	}
}

// Reference: pi v1.4.0 cli/list-models.ts copies getAvailable() into a fresh
// array before sorting. Rendering must not reorder the caller's model list.
func TestListModelsTextPreservesInputOrder(t *testing.T) {
	first := listModel("openai", "z", 1000, 1000, false, "text")
	second := listModel("anthropic", "a", 1000, 1000, false, "text")
	models := []*ai.Model{first, second}
	ListModelsText(models, "")
	if models[0] != first || models[1] != second {
		t.Fatal("rendering the table reordered the input models")
	}
}

// Reference: pi v1.4.0 cli/list-models.ts sorts providers and IDs with
// localeCompare, not byte order. D257 uses root collation for the Go equivalent.
func TestListModelsTextUsesLocaleOrdering(t *testing.T) {
	models := []*ai.Model{
		listModel("vendor-a", "b", 1000, 1000, false, "text"),
		listModel("vendor_a", "A", 1000, 1000, false, "text"),
		listModel("vendor_a", "a", 1000, 1000, false, "text"),
	}
	lines := strings.Split(strings.TrimSpace(ListModelsText(models, "")), "\n")
	want := []string{"vendor_a/a", "vendor_a/A", "vendor-a/b"}
	if len(lines) != len(want)+1 {
		t.Fatalf("table has %d lines, want %d", len(lines), len(want)+1)
	}
	for i, model := range want {
		fields := strings.Fields(lines[i+1])
		if got := fields[0] + "/" + fields[1]; got != model {
			t.Errorf("row %d = %q, want %q", i, got, model)
		}
	}
}

// pi v1.4.0 packages/coding-agent/src/cli/list-models.ts measures widths with
// JavaScript string.length, including two UTF-16 code units for each emoji.
func TestListModelsTextUsesUTF16ColumnWidths(t *testing.T) {
	models := []*ai.Model{listModel("😀😀😀😀😀", "m", 1, 1, false, "text")}
	got := ListModelsText(models, "")
	want := strings.Join([]string{
		"provider    model  context  max-out  thinking  images",
		"😀😀😀😀😀  m      1        1        no        no    ",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("table = %q, want %q", got, want)
	}
}

func TestListModelsTextNoModels(t *testing.T) {
	got := ListModelsText(nil, "")
	if !strings.HasPrefix(got, "No models available.") {
		t.Errorf("got %q, want the no-models guidance", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("got %q, want a trailing newline", got)
	}
}

func TestListModelsTextSearchFilter(t *testing.T) {
	models := []*ai.Model{
		listModel("anthropic", "claude-x", 1000, 1000, false, "text"),
		listModel("openai", "gpt-y", 1000, 1000, false, "text"),
	}

	// The pattern is matched against "provider id" and narrows the table.
	got := ListModelsText(models, "gpt")
	if !strings.Contains(got, "gpt-y") || strings.Contains(got, "claude-x") {
		t.Errorf("filtered table:\n%s", got)
	}

	// A pattern that matches nothing says so, naming the pattern.
	got = ListModelsText(models, "zzzzz")
	if got != "No models matching \"zzzzz\"\n" {
		t.Errorf("got %q", got)
	}

	// With no models at all the pattern is irrelevant: upstream reports the
	// empty catalog first.
	got = ListModelsText(nil, "zzzzz")
	if !strings.HasPrefix(got, "No models available.") {
		t.Errorf("got %q", got)
	}
}
