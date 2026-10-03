package durable

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Port of harness/registry.ts: the application-owned extension registry.

// sectionKeyPattern is upstream SECTION_KEY.
var sectionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// BuiltinTasks are the task definitions every registry holds; they are not an
// extension and cannot be removed or replaced.
//
// The task bodies land with the task runtime (harness/tool.ts,
// generation.ts, compaction.ts); the registry only reads their names and
// versions here.
var BuiltinTasks = []Task{
	{Definition: TaskDefinition{Name: RunTaskKind, Version: 1, Initial: emptyTaskInitial}},
	{Definition: TaskDefinition{Name: ToolTaskKind, Version: 1, Initial: emptyTaskInitial}},
	{Definition: TaskDefinition{Name: CompactionTaskKind, Version: 1, Initial: emptyTaskInitial}},
}

func emptyTaskInitial(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }

// registryState is the immutable published registry state.
type registryState struct {
	extensions []Extension
	byName     map[string]Extension
	tasks      map[string]Task
	taskOrder  []string
}

func newRegistryState(extensions []Extension) (*registryState, error) {
	state := &registryState{
		extensions: extensions,
		byName:     make(map[string]Extension, len(extensions)),
		tasks:      map[string]Task{},
	}
	for _, extension := range extensions {
		state.byName[extension.Name] = extension
	}
	for _, task := range BuiltinTasks {
		state.tasks[task.Definition.Name] = task
		state.taskOrder = append(state.taskOrder, task.Definition.Name)
	}
	for _, extension := range extensions {
		for _, task := range extension.Tasks {
			name := task.Definition.Name
			if _, present := state.tasks[name]; present {
				return nil, fmt.Errorf("Task %s of extension %s is already installed", name, extension.Name)
			}
			state.tasks[name] = task
			state.taskOrder = append(state.taskOrder, name)
		}
	}
	return state, nil
}

func (s *registryState) Installed() []Extension {
	return append([]Extension{}, s.extensions...)
}

func (s *registryState) Extension(name string) *Extension {
	if extension, ok := s.byName[name]; ok {
		return &extension
	}
	return nil
}

func (s *registryState) Tools() []RegistryTool {
	tools := []RegistryTool{}
	for _, extension := range s.extensions {
		for _, tool := range extension.Tools {
			tools = append(tools, RegistryTool{Extension: extension, Tool: tool})
		}
	}
	return tools
}

func (s *registryState) Sections() []RegistrySection {
	sections := []RegistrySection{}
	for _, extension := range s.extensions {
		for _, section := range extension.Sections {
			sections = append(sections, RegistrySection{Extension: extension, Section: section})
		}
	}
	return sections
}

func (s *registryState) Tasks() []Task {
	tasks := make([]Task, 0, len(s.taskOrder))
	for _, name := range s.taskOrder {
		tasks = append(tasks, s.tasks[name])
	}
	return tasks
}

func (s *registryState) Task(name string) *Task {
	if task, ok := s.tasks[name]; ok {
		return &task
	}
	return nil
}

// registryImpl is the mutable application-owned registry.
type registryImpl struct {
	current   *registryState
	listeners map[int]func()
	nextID    int
}

// NewRegistry creates an application-owned registry holding only the built-in
// tasks.
func NewRegistry() Registry {
	state, _ := newRegistryState(nil)
	return &registryImpl{current: state, listeners: map[int]func(){}}
}

func (r *registryImpl) Snapshot() RegistrySnapshot { return r.current }

func (r *registryImpl) Subscribe(listener func()) func() {
	id := r.nextID
	r.nextID++
	r.listeners[id] = listener
	return func() { delete(r.listeners, id) }
}

// Install installs extension, replacing the installed extension with its name
// in place and publishing at once.
func (r *registryImpl) Install(extension Extension) error {
	if err := ValidateExtension(extension); err != nil {
		return err
	}
	current := r.current.extensions
	index := -1
	for at, installed := range current {
		if installed.Name == extension.Name {
			index = at
			break
		}
	}
	next := make([]Extension, 0, len(current)+1)
	if index < 0 {
		next = append(next, current...)
		next = append(next, extension)
	} else {
		next = append(next, current...)
		next[index] = extension
	}
	return r.publish(next)
}

// Uninstall removes the installed extension with extension.Name, whichever
// object it is.
func (r *registryImpl) Uninstall(extension Extension) error {
	current := r.current.extensions
	found := false
	next := make([]Extension, 0, len(current))
	for _, installed := range current {
		if installed.Name == extension.Name {
			found = true
			continue
		}
		next = append(next, installed)
	}
	if !found {
		return nil
	}
	return r.publish(next)
}

// publish builds and validates the next state, which fails on a task name
// collision, then publishes it synchronously.
func (r *registryImpl) publish(extensions []Extension) error {
	state, err := newRegistryState(extensions)
	if err != nil {
		return err
	}
	r.current = state
	listeners := make([]func(), 0, len(r.listeners))
	for _, listener := range r.listeners {
		listeners = append(listeners, listener)
	}
	for _, listener := range listeners {
		listener()
	}
	return nil
}

// ValidateExtension enforces unique tool names and section keys within one
// extension, and valid, unreserved section keys.
func ValidateExtension(extension Extension) error {
	tools := map[string]bool{}
	for _, tool := range extension.Tools {
		if tools[tool.Name] {
			return fmt.Errorf("Extension %s has two tools named %s", extension.Name, tool.Name)
		}
		tools[tool.Name] = true
	}
	sections := map[string]bool{}
	for _, section := range extension.Sections {
		if !sectionKeyPattern.MatchString(section.Key) {
			return fmt.Errorf("Section key %q must match %s", section.Key, sectionKeyPattern.String())
		}
		if section.Key == InstructionsKey {
			return fmt.Errorf("Section key %s is reserved for the agent's instructions", section.Key)
		}
		if sections[section.Key] {
			return fmt.Errorf("Extension %s has two sections with key %s", extension.Name, section.Key)
		}
		sections[section.Key] = true
	}
	return nil
}
