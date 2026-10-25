package coding

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/dat267/pier/ai"
)

type branchPrepConverted struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// TestBranchPreparationMatchesUpstream pins branch-summary preparation and the
// LLM projection of its messages, including `!` bash executions (prepared and
// rendered as user text) and `!!` ones (prepared, then excluded by ConvertToLlm).
// Regenerate with
// node coding/testdata/branch-preparation/generate.mjs ./pi > coding/testdata/branch-preparation/golden.json
func TestBranchPreparationMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/branch-preparation/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]struct {
		Entries       []SessionEntry        `json:"entries"`
		Budget        int                   `json:"budget"`
		PreparedRoles []string              `json:"preparedRoles"`
		Converted     []branchPrepConverted `json:"converted"`
		FileOps       struct {
			Read    []string `json:"read"`
			Written []string `json:"written"`
			Edited  []string `json:"edited"`
		} `json:"fileOps"`
		TotalTokens int `json:"totalTokens"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture) == 0 {
		t.Fatal("fixture is empty")
	}
	for name, test := range fixture {
		t.Run(name, func(t *testing.T) {
			preparation := PrepareBranchEntries(test.Entries, test.Budget)
			roles := make([]string, 0, len(preparation.Messages))
			for _, message := range preparation.Messages {
				roles = append(roles, messageRole(message))
			}
			if !reflect.DeepEqual(roles, test.PreparedRoles) {
				t.Errorf("prepared roles = %v, want %v", roles, test.PreparedRoles)
			}
			converted := make([]branchPrepConverted, 0, len(preparation.Messages))
			for _, message := range ConvertToLlm(preparation.Messages) {
				converted = append(converted, branchPrepConverted{Role: messageRole(message), Text: messageText(message)})
			}
			if !reflect.DeepEqual(converted, test.Converted) {
				t.Errorf("converted = %+v, want %+v", converted, test.Converted)
			}
			fileOps := struct {
				Read    []string `json:"read"`
				Written []string `json:"written"`
				Edited  []string `json:"edited"`
			}{Read: sortedSetKeys(preparation.FileOps.Read), Written: sortedSetKeys(preparation.FileOps.Written), Edited: sortedSetKeys(preparation.FileOps.Edited)}
			if !reflect.DeepEqual(fileOps, test.FileOps) {
				t.Errorf("fileOps = %+v, want %+v", fileOps, test.FileOps)
			}
			if preparation.TotalTokens != test.TotalTokens {
				t.Errorf("totalTokens = %d, want %d", preparation.TotalTokens, test.TotalTokens)
			}
		})
	}
}

func messageRole(message ai.Message) string {
	switch m := message.(type) {
	case *ai.UserMessage:
		return "user"
	case *ai.AssistantMessage:
		return "assistant"
	case *ai.ToolResultMessage:
		return "toolResult"
	case *ai.SystemMessage:
		return "system"
	case *ai.CustomMessage:
		return m.Role
	default:
		return "unsupported"
	}
}

func messageText(message ai.Message) string {
	var content ai.StringOrBlocks
	switch m := message.(type) {
	case *ai.UserMessage:
		content = m.Content
	case *ai.AssistantMessage:
		content = ai.StringOrBlocks{Blocks: m.Content}
	case *ai.ToolResultMessage:
		content = StringOrBlocksOf(m.Content)
	case *ai.SystemMessage:
		content = m.Content
	default:
		return ""
	}
	return contentTextJoinedNewline(content)
}

func sortedSetKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}
