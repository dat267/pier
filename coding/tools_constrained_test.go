package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
)

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
