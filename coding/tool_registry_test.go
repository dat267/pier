package coding

import (
	"encoding/json"
	"fmt"
	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"os"
	"strings"
	"sync"
	"testing"
)

// pi v1.4.0 (4a080252), core/agent-session.ts _rebuildSystemPrompt and
// _refreshToolRegistry execute on one JS thread. Late MCP attachment runs on
// a Go worker, so prompt resolution must read the registry under its lock.
func TestSystemPromptCanReadDuringMcpToolAttachment(t *testing.T) {
	dir := t.TempDir()
	tools := []agent.AgentTool{{Name: "read", Parameters: []byte(`{"type":"object"}`)}}
	for i := range 64 {
		tools = append(tools, agent.AgentTool{Name: fmt.Sprintf("fixture_%d", i), Parameters: []byte(`{"type":"object"}`)})
	}
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: InMemorySession(dir, nil, nil), StreamFn: stubStreamFn, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Go(func() {
		<-start
		for range 128 {
			session.AttachExtraTools([]agent.AgentTool{{Name: "mcp__fixture__late", Parameters: []byte(`{"type":"object"}`)}})
			session.AttachExtraTools(nil)
		}
	})
	wait.Go(func() {
		<-start
		for range 128 {
			if prompt := session.SystemPrompt(); !strings.Contains(prompt, "Read file contents") {
				t.Error("stable read tool disappeared from the prompt")
				return
			}
		}
	})
	close(start)
	wait.Wait()
}

// Upstream read.ts, write.ts, edit.ts and bash.ts declare
// constrainedSampling: { type: "json_schema", strict: "prefer" }
// (tool-definition-wrapper.ts carries it onto AgentTool) and
// ai/src/utils/transcript.ts's toToolDeclaration keeps it in the model-facing
// declaration. find, grep and ls declare none.
func TestBuiltInToolsCarryConstrainedSampling(t *testing.T) {
	cwd := t.TempDir()
	prefer := map[string]agent.AgentTool{
		"read":       CreateReadTool(cwd, nil),
		"write":      CreateWriteTool(cwd),
		"edit":       CreateEditTool(cwd),
		"bash":       CreateBashTool(cwd, nil),
		"powershell": CreatePowerShellTool(cwd, nil),
	}
	for name, tool := range prefer {
		decl := tool.ToolDeclaration()
		cfg := decl.ConstrainedSampling
		if !cfg.Set || cfg.False || cfg.Config == nil || cfg.Config.Type != "json_schema" || cfg.Config.Strict != "prefer" {
			t.Fatalf("%s declaration constrainedSampling = %+v; want json_schema/prefer", name, cfg)
		}
		wire := string(mustMarshalJSON(decl))
		if !strings.Contains(wire, `"constrainedSampling":{"type":"json_schema","strict":"prefer"}`) {
			t.Fatalf("%s declaration wire = %s", name, wire)
		}
	}
	none := map[string]agent.AgentTool{
		"find": CreateFindTool(cwd),
		"grep": CreateGrepTool(cwd),
		"ls":   CreateLsTool(cwd),
	}
	for name, tool := range none {
		decl := tool.ToolDeclaration()
		if decl.ConstrainedSampling.Set {
			t.Fatalf("%s declaration unexpectedly carries constrainedSampling: %+v", name, decl.ConstrainedSampling)
		}
		if strings.Contains(string(mustMarshalJSON(decl)), "constrainedSampling") {
			t.Fatalf("%s declaration wire includes constrainedSampling", name)
		}
	}
}

// pi v1.4.0 (4a080252), core/tools/{read,write,edit,bash,grep,find,ls}.ts:
// their TypeBox schemas keep construction order at every object level. These
// bytes are sent in provider declarations and persisted in system tool deltas.
// Regenerate: node testdata/tool-schemas/generate.mjs ../pi > testdata/tool-schemas/golden.json
func TestBuiltinToolSchemasMatchUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/tool-schemas/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Name, Parameters string }
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			tool, err := CreateTool(fixture.Name, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := ai.MarshalJSON(tool.Parameters)
			if err != nil {
				t.Fatal(err)
			}
			want := fixture.Parameters
			if fixture.Name == "bash" {
				// D407: the timeout has a default, so its description names it.
				want = strings.Replace(want, "(optional, no default timeout)", "(optional, defaults to 600)", 1)
			}
			if string(encoded) != want {
				t.Fatalf("parameters = %s\nwant         %s", encoded, want)
			}
		})
	}
}
