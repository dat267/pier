package coding

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/dat267/pier/ai"
)

// Port of core/system-prompt.ts: system prompt construction and project
// context loading.

// SystemPromptSectionName validates section names.
var systemPromptSectionName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// BuildSystemPromptOptions are the prompt inputs.
type BuildSystemPromptOptions struct {
	// CustomPrompt replaces the default prefix.
	CustomPrompt string
	// ForceSystemPrompt is an exact full prompt replacement.
	ForceSystemPrompt string
	// ForceSystemPromptSet distinguishes an explicitly empty replacement from unset.
	ForceSystemPromptSet bool
	// SelectedTools to include in the prompt (default: read, bash, edit, write).
	SelectedTools []string
	// HiddenTools are selected tools whose declarations are omitted from the prompt.
	HiddenTools []string
	// ToolSnippets: one-line tool descriptions keyed by tool name.
	ToolSnippets map[string]string
	// ToolGuidelines: guideline bullets per tool.
	ToolGuidelines map[string][]string
	// PromptGuidelines appended to the default rules.
	PromptGuidelines []string
	// AppendSystemPrompt is appended before project context/skills/cwd.
	AppendSystemPrompt string
	// PromptSourcePaths are the loaded system/append prompt files (the
	// loaded-resources Context section lists them ahead of the context files).
	PromptSourcePaths []string
	// Sections: additional XML-wrapped prompt sections keyed by tag name.
	Sections map[string]string
	// CustomSectionOrder preserves the insertion order of Sections; omitted names sort deterministically.
	CustomSectionOrder []string
	// Cwd is the working directory.
	Cwd string
	// ContextFiles are pre-loaded context files.
	ContextFiles []ContextFile
	// Skills are pre-loaded skills.
	Skills []Skill
}

// ContextFile is one project context file.
type ContextFile struct {
	Path    string
	Content string
}

// SystemPromptSections are the ordered, independently replaceable prompt
// sections keyed by name (preamble untagged, the rest XML-wrapped).
type SystemPromptSections map[string]string

// GetPackageDir resolves the pi package directory (docs/README paths). The
// Go port uses PI_PACKAGE_DIR when set, else the module root discovered at
// build time is unavailable — docs paths resolve relative to PI_PACKAGE_DIR
// only (upstream resolves its npm layout).
var (
	packageDirMu sync.Mutex
	packageDir   string
)

// SetPackageDir sets the docs root (host wiring).
func SetPackageDir(dir string) {
	packageDirMu.Lock()
	defer packageDirMu.Unlock()
	packageDir = dir
}

func currentPackageDir() string {
	packageDirMu.Lock()
	defer packageDirMu.Unlock()
	return packageDir
}

func GetReadmePath() string { return resolveIn(currentPackageDir(), "README.md") }
func GetDocsPath() string   { return resolveIn(currentPackageDir(), "docs") }

// GetChangelogPath is the CHANGELOG.md path.
func GetChangelogPath() string { return resolveIn(currentPackageDir(), "CHANGELOG.md") }

// GetDebugLogPath is the interactive debug log path.
func GetDebugLogPath() string { return filepath.Join(GetAgentDir(), AppName+"-debug.log") }

func resolveIn(base, name string) string {
	if base == "" {
		return name
	}
	return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), name)
}

func renderProjectContext(contextFiles []ContextFile) string {
	parts := []string{"Project-specific instructions and guidelines:"}
	for _, file := range contextFiles {
		parts = append(parts, fmt.Sprintf("<project_instructions path=\"%s\">\n%s\n</project_instructions>", file.Path, file.Content))
	}
	return strings.Join(parts, "\n\n")
}

func buildRules(selectedTools []string, toolGuidelines map[string][]string, promptGuidelines []string) string {
	var rules []string
	seen := map[string]bool{}
	addRule := func(rule string) {
		normalized := ai.JSTrim(rule)
		if normalized == "" || seen[normalized] {
			return
		}
		seen[normalized] = true
		rules = append(rules, normalized)
	}

	hasBash := contains(selectedTools, "bash")
	hasPowerShell := contains(selectedTools, "powershell")
	hasGrep := contains(selectedTools, "grep")
	hasFind := contains(selectedTools, "find")
	hasLs := contains(selectedTools, "ls")

	if (hasBash || hasPowerShell) && !hasGrep && !hasFind && !hasLs {
		switch {
		case hasBash && hasPowerShell:
			addRule("Use bash or PowerShell for file operations like listing, searching, and finding files")
		case hasPowerShell:
			addRule("Use PowerShell for file operations like listing, searching, and finding files")
		default:
			addRule("Use bash for file operations like ls, rg, find")
		}
	}

	// D286: keep file writes on the file tools. Upstream's tool contributions
	// never say this, so a model may edit through shell redirection or sed;
	// jcode's prompt states the same rule ("Edit files with edit, replace,
	// apply_patch, or write, not sed, perl, or Python scripts in bash").
	if hasBash || hasPowerShell {
		var fileTools []string
		if contains(selectedTools, ToolNameEdit) {
			fileTools = append(fileTools, ToolNameEdit)
		}
		if contains(selectedTools, ToolNameWrite) {
			fileTools = append(fileTools, ToolNameWrite)
		}
		if len(fileTools) > 0 {
			shell := "bash"
			if !hasBash {
				shell = "PowerShell"
			}
			addRule(fmt.Sprintf("Do not write or edit files through %s (redirection, heredocs, sed, perl, python); use %s.",
				shell, strings.Join(fileTools, " or ")))
		}
	}

	for _, name := range selectedTools {
		for _, rule := range toolGuidelines[name] {
			addRule(rule)
		}
	}
	for _, rule := range promptGuidelines {
		addRule(rule)
	}
	addRule("Be concise in your responses")
	addRule("Show file paths clearly when working with files")
	var lines []string
	for _, rule := range rules {
		lines = append(lines, "- "+rule)
	}
	return strings.Join(lines, "\n")
}

func contains(list []string, want string) bool { return slices.Contains(list, want) }

// BuildSystemPromptSections builds the ordered prompt sections
// (port of buildSystemPromptSections).
func BuildSystemPromptSections(input BuildSystemPromptOptions) (SystemPromptSections, error) {
	// Only an omitted list gets the default tools (upstream's `??`): an
	// explicit empty list means the session has no tools, so the rules and the
	// skill-reader check must see it empty. Session callers always pass the
	// active tool names, which is a non-nil (possibly empty) list.
	selectedTools := input.SelectedTools
	if selectedTools == nil {
		selectedTools = []string{"read", "bash", "edit", "write"}
	}
	hiddenTools := make(map[string]bool, len(input.HiddenTools))
	for _, name := range input.HiddenTools {
		hiddenTools[name] = true
	}
	declaredTools := make([]string, 0, len(selectedTools))
	for _, name := range selectedTools {
		if !hiddenTools[name] {
			declaredTools = append(declaredTools, name)
		}
	}
	toolSnippets := input.ToolSnippets
	toolGuidelines := input.ToolGuidelines
	promptGuidelines := input.PromptGuidelines
	appendSystemPrompt := input.AppendSystemPrompt
	customSections := input.Sections
	cwd := input.Cwd
	contextFiles := input.ContextFiles
	skills := input.Skills

	for name := range customSections {
		if !systemPromptSectionName.MatchString(name) || name == "preamble" {
			return nil, fmt.Errorf("Invalid system prompt section name: %s", name)
		}
	}

	promptSections := SystemPromptSections{}
	if input.CustomPrompt != "" {
		promptSections["preamble"] = input.CustomPrompt
	} else {
		promptSections["preamble"] = "You are an expert coding assistant operating inside pi, a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files."
		var visibleTools []string
		for _, name := range declaredTools {
			if toolSnippets[name] != "" {
				visibleTools = append(visibleTools, name)
			}
		}
		var tools string
		if len(visibleTools) > 0 {
			var lines []string
			for _, name := range visibleTools {
				lines = append(lines, fmt.Sprintf("- %s: %s", name, toolSnippets[name]))
			}
			tools = strings.Join(lines, "\n")
		} else {
			tools = "(none)"
		}
		promptSections["tools"] = tools + "\n\nIn addition to the tools above, you may have access to other custom tools depending on the project."
		promptSections["rules"] = buildRules(declaredTools, toolGuidelines, promptGuidelines)
		// D176: upstream emits a `docs` section (the Pi documentation index).
		// It routed the model to pi's docs/examples tree, which a pier install
		// does not ship — the routing table names extensions.md, themes.md,
		// sdk.md, packages.md, custom-provider.md and the like, none of which
		// exist here — so the section only pointed at missing files.
	}

	if appendSystemPrompt != "" {
		promptSections["addendum"] = appendSystemPrompt
	}
	if len(contextFiles) > 0 {
		promptSections["project_context"] = renderProjectContext(contextFiles)
	}
	skillFileReadTool := ""
	for _, tool := range []string{"read", "bash"} {
		if contains(declaredTools, tool) {
			skillFileReadTool = tool
			break
		}
	}
	if skillFileReadTool == "" && (contains(selectedTools, "read") || contains(selectedTools, "bash")) {
		skillFileReadTool = "indirect"
	}
	if skillFileReadTool != "" && len(skills) > 0 {
		skillsPrompt := ai.JSTrim(FormatSkillsForPrompt(skills, skillFileReadTool))
		if skillsPrompt != "" {
			promptSections["skills"] = skillsPrompt
		}
	}
	promptSections["cwd"] = strings.ReplaceAll(cwd, "\\", "/")
	for name, content := range customSections {
		if content != "" {
			promptSections[name] = content
		}
	}

	sections := SystemPromptSections{"preamble": promptSections["preamble"]}
	// Upstream renders sections in insertion order: preamble, tools, rules,
	// addendum, project_context, skills, cwd, then custom sections (D176 drops
	// upstream's `docs` section). CustomSectionOrder carries custom map order.
	knownOrder := []string{"tools", "rules", "addendum", "project_context", "skills", "cwd"}
	rendered := map[string]bool{"preamble": true}
	for _, name := range knownOrder {
		if content, ok := promptSections[name]; ok {
			sections[name] = fmt.Sprintf("<%s>\n%s\n</%s>", name, content, name)
			rendered[name] = true
		}
	}
	var customNames []string
	for name := range promptSections {
		if !rendered[name] {
			customNames = append(customNames, name)
		}
	}
	sortNames(customNames)
	for _, name := range customNames {
		sections[name] = fmt.Sprintf("<%s>\n%s\n</%s>", name, promptSections[name], name)
	}
	return sections, nil
}

// sectionInsertionOrder recovers the render order: known sections first,
// then custom sections in caller order, with unspecified names sorted.
func sectionInsertionOrder(sections SystemPromptSections, customOrder []string) []string {
	knownOrder := []string{"tools", "rules", "addendum", "project_context", "skills", "cwd"}
	var out []string
	seen := map[string]bool{"preamble": true}
	for _, name := range knownOrder {
		if _, ok := sections[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	for _, name := range customOrder {
		if _, ok := sections[name]; ok && !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	// D301: names without an explicit order have no Go map insertion order;
	// sort them deterministically after the caller-supplied names.
	var custom []string
	for name := range sections {
		if !seen[name] {
			custom = append(custom, name)
		}
	}
	sortNames(custom)
	return append(out, custom...)
}

func sortNames(names []string) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}

// BuildSystemPromptState returns the complete prompt state: a forced prompt
// is opaque (content only, no sections); otherwise the structured sections
// carry the prompt.
func BuildSystemPromptState(input BuildSystemPromptOptions) (content string, sections SystemPromptSections, err error) {
	if input.ForceSystemPromptSet || input.ForceSystemPrompt != "" {
		return input.ForceSystemPrompt, nil, nil
	}
	sections, err = BuildSystemPromptSections(input)
	if err != nil {
		return "", nil, err
	}
	return "", sections, nil
}

// BuildSystemPrompt renders the prompt text exactly as the transcript's
// system message replays it.
func BuildSystemPrompt(input BuildSystemPromptOptions) (string, error) {
	content, sections, err := BuildSystemPromptState(input)
	if err != nil {
		return "", err
	}
	// getSystemMessageText: content followed by section values in
	// insertion order — preamble first, then the section order fixed above.
	parts := []string{content}
	if sections != nil {
		orderedNames := append([]string{"preamble"}, sectionInsertionOrder(sections, input.CustomSectionOrder)...)
		for _, name := range orderedNames {
			if value, ok := sections[name]; ok {
				parts = append(parts, value)
			}
		}
	}
	var nonEmpty []string
	for _, part := range parts {
		if len(part) > 0 {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "\n\n"), nil
}

// DiffSystemPromptSections diffs the model's current sections against the
// desired ones, returning a SystemMessage.sections patch (nil = no change).
func DiffSystemPromptSections(previous map[string]string, current SystemPromptSections) map[string]*string {
	patch := map[string]*string{}
	for name, text := range current {
		if previous[name] != text {
			value := text
			patch[name] = &value
		}
	}
	for name := range previous {
		if _, ok := current[name]; !ok {
			patch[name] = nil
		}
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}
