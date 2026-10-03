package durable

import (
	"testing"

	"github.com/dat267/pier/chord"
)

// Port of harness/registry.ts.

func testSection(key string) PromptSection {
	return PromptSection{Key: key, Render: func(PromptInput, chord.Context) (string, bool, error) { return "", false, nil }}
}

func TestRegistryBuiltinTasks(t *testing.T) {
	registry := NewRegistry()
	tasks := registry.Snapshot().Tasks()
	if len(tasks) != 3 || tasks[0].Definition.Name != RunTaskKind ||
		tasks[1].Definition.Name != ToolTaskKind || tasks[2].Definition.Name != CompactionTaskKind {
		t.Fatalf("tasks = %+v", tasks)
	}
	if registry.Snapshot().Task(RunTaskKind) == nil || registry.Snapshot().Extension("missing") != nil {
		t.Fatal("lookups wrong")
	}
}

func TestRegistryInstallUninstallPublish(t *testing.T) {
	registry := NewRegistry()
	notifications := 0
	unsubscribe := registry.Subscribe(func() { notifications++ })
	extension := Extension{Name: "a", Tools: []ToolRegistration{testTool("x", "")},
		Sections: []PromptSection{testSection("s1")}}
	if err := registry.Install(extension); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("notifications = %d", notifications)
	}
	snapshot := registry.Snapshot()
	if len(snapshot.Installed()) != 1 || snapshot.Extension("a") == nil {
		t.Fatalf("installed = %+v", snapshot.Installed())
	}
	if len(snapshot.Tools()) != 1 || snapshot.Tools()[0].Tool.Name != "x" {
		t.Fatalf("tools = %+v", snapshot.Tools())
	}
	if len(snapshot.Sections()) != 1 || snapshot.Sections()[0].Section.Key != "s1" {
		t.Fatalf("sections = %+v", snapshot.Sections())
	}
	// A second extension installs after the first.
	if err := registry.Install(Extension{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	// Replacing keeps the position and the object is the new one.
	replacement := Extension{Name: "a", Tools: []ToolRegistration{testTool("y", "")}}
	if err := registry.Install(replacement); err != nil {
		t.Fatal(err)
	}
	snapshot = registry.Snapshot()
	if len(snapshot.Installed()) != 2 || snapshot.Installed()[0].Name != "a" ||
		snapshot.Tools()[0].Tool.Name != "y" {
		t.Fatalf("installed = %+v", snapshot.Installed())
	}
	// Uninstalling a missing extension publishes nothing; the real one does.
	before := notifications
	if err := registry.Uninstall(Extension{Name: "missing"}); err != nil {
		t.Fatal(err)
	}
	if notifications != before {
		t.Fatal("missing uninstall must not publish")
	}
	if err := registry.Uninstall(Extension{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if len(registry.Snapshot().Installed()) != 1 {
		t.Fatalf("installed = %+v", registry.Snapshot().Installed())
	}
	unsubscribe()
	after := notifications
	_ = registry.Install(Extension{Name: "c"})
	if notifications != after {
		t.Fatal("unsubscribed listener must not fire")
	}
}

func TestRegistryValidatesExtension(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Install(Extension{Name: "a", Tools: []ToolRegistration{testTool("x", ""), testTool("x", "")}}); err == nil {
		t.Fatal("duplicate tool names must fail")
	}
	if err := registry.Install(Extension{Name: "a", Sections: []PromptSection{testSection("Bad")}}); err == nil {
		t.Fatal("an invalid section key must fail")
	}
	if err := registry.Install(Extension{Name: "a", Sections: []PromptSection{testSection(InstructionsKey)}}); err == nil {
		t.Fatal("the reserved section key must fail")
	}
	if err := registry.Install(Extension{Name: "a", Sections: []PromptSection{testSection("s"), testSection("s")}}); err == nil {
		t.Fatal("duplicate section keys must fail")
	}
	// A failed install leaves the registry unchanged.
	if len(registry.Snapshot().Installed()) != 0 {
		t.Fatalf("installed = %+v", registry.Snapshot().Installed())
	}
}

func TestRegistryRejectsTaskCollision(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Install(Extension{Name: "a", Tasks: []Task{{Definition: TaskDefinition{Name: RunTaskKind, Version: 1}}}}); err == nil {
		t.Fatal("a built-in task name must not be replaceable")
	}
	if err := registry.Install(Extension{Name: "a", Tasks: []Task{{Definition: TaskDefinition{Name: "custom", Version: 1}}}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(Extension{Name: "b", Tasks: []Task{{Definition: TaskDefinition{Name: "custom", Version: 1}}}}); err == nil {
		t.Fatal("two extensions must not install the same task name")
	}
	if registry.Snapshot().Task("custom") == nil {
		t.Fatal("the custom task must be installed")
	}
}
