package coding

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func writeSkill(t *testing.T, dir string, name string, description string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeMarkdownSkill writes one non-SKILL.md skill file whose declared name
// comes from the frontmatter.
func writeMarkdownSkill(t *testing.T, dir string, fileName string, name string, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody\n"
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setHomeDir pins the home directory for both of os.UserHomeDir's inputs:
// HOME on Unix and USERPROFILE on Windows. Pinning only HOME left a Windows run
// reading the developer's real ~/.pi, so these tests loaded the real skills dir
// alongside the fixture and saw extra entries.
func setHomeDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// utils/frontmatter.ts trims the extracted body with JavaScript String.trim.
func TestParseFrontmatterTrimsJavaScriptWhitespaceFromBody(t *testing.T) {
	got := ParseFrontmatter("---\nname: test\n---\n" + "\uFEFF" + "body" + "\uFEFF").Body
	if got != "body" {
		t.Fatalf("body = %q; want JavaScript-trimmed body", got)
	}
}

// core/skills.ts validateDescription uses JavaScript String.trim, including BOM.
func TestLoadSkillTreatsBOMOnlyDescriptionAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bom", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: bom\ndescription: \"" + "\uFEFF" + "\"\n---\nbody"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, diagnostics := loadSkillFromFile(path, "user")
	if skill != nil || len(diagnostics) != 1 || diagnostics[0].Message != "description is required" {
		t.Fatalf("skill = %+v, diagnostics = %+v; want BOM-only description rejected", skill, diagnostics)
	}
}

// core/skills.ts treats disable-model-invocation as boolean true only; YAML
// quoted `"true"` remains a string under utils/frontmatter.ts's yaml parser.
func TestLoadSkillQuotedBooleanDoesNotDisableModelInvocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quoted", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: quoted
description: test
disable-model-invocation: "true"
---
body`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, _ := loadSkillFromFile(path, "user")
	if skill == nil || skill.DisableModelInvocation {
		t.Fatalf("skill = %+v, want quoted string true not to disable model invocation", skill)
	}
}

// core/skills.ts validateDescription warns above MAX_DESCRIPTION_LENGTH (1024).
func TestLoadSkillWarnsWhenDescriptionExceedsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	description := strings.Repeat("x", 1025)
	content := "---\nname: long\ndescription: " + description + "\n---\nbody"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, diagnostics := loadSkillFromFile(path, "user")
	if skill == nil {
		t.Fatal("long description should warn but keep loading the skill")
	}
	if len(diagnostics) != 1 || diagnostics[0].Message != "description exceeds 1024 characters (1025)" {
		t.Fatalf("diagnostics = %+v, want description length warning", diagnostics)
	}
}

// skills.ts validates names using JavaScript String.length (UTF-16 code units).
func TestValidateSkillNameCountsUTF16(t *testing.T) {
	name := strings.Repeat("😀", 32)
	got := validateSkillName(name)
	if len(got) != 1 || got[0] != "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)" {
		t.Fatalf("validation errors = %v, want only invalid-character warning", got)
	}
}

// yaml parser in utils/frontmatter.ts recognizes case-insensitive YAML booleans.
func TestLoadSkillYAMLTrueVariantsDisableModelInvocation(t *testing.T) {
	for _, value := range []string{"true", "True", "TRUE", "true # comment"} {
		path := filepath.Join(t.TempDir(), value, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: variant\ndescription: test\ndisable-model-invocation: " + value + "\n---\nbody"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		skill, _ := loadSkillFromFile(path, "user")
		if skill == nil || !skill.DisableModelInvocation {
			t.Errorf("value %q produced skill %+v; want model invocation disabled", value, skill)
		}
	}
}

// core/skills.ts validateDescription requires a string, not a YAML number.
// utils/frontmatter.ts uses YAML's core schema, where bare NaN is a string.
func TestParseFrontmatterTreatsBareNaNAsString(t *testing.T) {
	parsed := ParseFrontmatter("---\ndescription: NaN\n---\nbody")
	if parsed.NonStrings["description"] {
		t.Fatal("bare NaN must remain a YAML string")
	}
}

func TestLoadSkillRejectsNonStringDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numeric", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: numeric\ndescription: 123\n---\nbody"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, diagnostics := loadSkillFromFile(path, "user")
	if skill != nil || len(diagnostics) != 1 || diagnostics[0].Message != "description is required" {
		t.Fatalf("skill = %+v, diagnostics = %+v; want missing-string warning and no skill", skill, diagnostics)
	}
}

// core/skills.ts reads skill files as UTF-8 and replaces malformed sequences.
func TestLoadSkillReplacesMalformedUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := append([]byte("---\nname: demo\ndescription: test"), 0xff)
	content = append(content, []byte("\n---\nbody")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	skill, _ := loadSkillFromFile(path, "user")
	if skill == nil || skill.Description != "test\uFFFD" {
		var got string
		if skill != nil {
			got = skill.Description
		}
		t.Fatalf("skill description = %q; want replacement-decoded text", got)
	}
}

// skills.ts receives UTF-8-decoded directory entry names from readdir.
func TestLoadSkillsSkipsInvalidUTF8DirectoryNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow invalid UTF-8 file names")
	}
	root := t.TempDir()
	invalidDir := filepath.Join(root, string([]byte{0xff})+"skill")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, "SKILL.md"), []byte("---\nname: bad\ndescription: skill\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 0 {
		t.Fatalf("skills from invalid UTF-8 path = %+v; want skipped", result.Skills)
	}
}

// core/skills.ts canonicalizes real paths so the default skills directory and
// an explicit path to the same directory load each skill only once.
func TestLoadSkillsDedupesDefaultsAndPaths(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	agentDir := filepath.Join(home, ".pi", "agent")
	writeSkill(t, filepath.Join(agentDir, "skills"), "tdd", "TDD")

	result := LoadSkills(LoadSkillsOptions{
		Cwd: t.TempDir(), AgentDir: agentDir,
		SkillPaths: []string{"~/.pi/agent/skills"}, IncludeDefaults: true,
	}, false)

	if len(result.Skills) != 1 || result.Skills[0].Name != "tdd" {
		t.Fatalf("skills = %+v, want exactly one tdd", result.Skills)
	}
	for _, diagnostic := range result.Diagnostics {
		t.Fatalf("unexpected diagnostic: %+v", diagnostic)
	}
}

// TestLoadSkillsCollisionDiagnostic covers upstream's name-collision warning:
// two skills with the same name keep the first and record the loser.
func TestLoadSkillsCollisionDiagnostic(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	winner := writeSkill(t, dirA, "dup", "first")
	loser := writeSkill(t, dirB, "dup", "second")

	result := LoadSkills(LoadSkillsOptions{
		AgentDir: t.TempDir(), SkillPaths: []string{dirA, dirB},
	}, false)

	if len(result.Skills) != 1 || result.Skills[0].FilePath != winner {
		t.Fatalf("skills = %+v, want only %s", result.Skills, winner)
	}
	var collision *ResourceDiagnostic
	for index := range result.Diagnostics {
		if result.Diagnostics[index].Type == "collision" {
			collision = &result.Diagnostics[index]
		}
	}
	if collision == nil {
		t.Fatalf("missing collision diagnostic: %+v", result.Diagnostics)
	}
	if collision.Collision == nil || collision.Collision.WinnerPath != winner || collision.Collision.LoserPath != loser {
		t.Fatalf("collision = %+v", collision.Collision)
	}
}

// TestLoadSkillsResolvesTildePath covers upstream's resolvePath on skill paths:
// a "~" path resolves instead of producing a "does not exist" warning.
func TestLoadSkillsResolvesTildePath(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	skillsDir := filepath.Join(home, "myskills")
	writeSkill(t, skillsDir, "custom", "custom")

	result := LoadSkills(LoadSkillsOptions{
		AgentDir: t.TempDir(), SkillPaths: []string{"~/myskills"},
	}, false)

	if len(result.Skills) != 1 || result.Skills[0].Name != "custom" {
		t.Fatalf("skills = %+v", result.Skills)
	}
	if result.Skills[0].SourceInfo.Scope != SourceScopeTemporary {
		t.Fatalf("scope = %q, want temporary", result.Skills[0].SourceInfo.Scope)
	}
}

// The project's .pi/skills is read only when the project is trusted
// (upstream's resource loader gates project paths on isProjectTrusted; the port
// threads the same flag through LoadSkills). Without this the trust decision at
// startup would decide nothing, and an untrusted checkout's skills would reach
// the system prompt.
func TestLoadSkillsSkipsUntrustedProject(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	agentDir := filepath.Join(home, ".pi", "agent")

	cwd := t.TempDir()
	writeSkill(t, filepath.Join(cwd, ConfigDirName, "skills"), "project-skill", "project")

	untrusted := LoadSkills(LoadSkillsOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true}, false)
	for _, skill := range untrusted.Skills {
		if skill.Name == "project-skill" {
			t.Fatalf("an untrusted project's skill was loaded: %+v", untrusted.Skills)
		}
	}

	trusted := LoadSkills(LoadSkillsOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true}, true)
	found := false
	for _, skill := range trusted.Skills {
		if skill.Name == "project-skill" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a trusted project's skill was not loaded: %+v", trusted.Skills)
	}
}

// skillNames lists the names in order.
func skillNames(result LoadSkillsResult) []string {
	names := make([]string, 0, len(result.Skills))
	for _, skill := range result.Skills {
		names = append(names, skill.Name)
	}
	return names
}

// TestLoadSkillsFromDirScansInReadDirOrder pins the depth-first order the
// bounded walk must preserve: the sequential walk appended each skill as it
// read it, so the parallel scan may not reorder them. Entries are visited in
// ReadDir order, root SKILL.md files short-circuit their directory, and
// nested ordinary Markdown files, dotfiles, node_modules and non-markdown files stay skipped.
func TestLoadSkillsFromDirScansInReadDirOrder(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "skill a") // a/SKILL.md: root short-circuit
	writeMarkdownSkill(t, filepath.Join(root, "b"), "a.md", "x", "skill x")
	writeSkill(t, filepath.Join(root, "b"), "z", "sub") // b/z/SKILL.md
	writeMarkdownSkill(t, root, "c.md", "c", "skill c")
	writeSkill(t, filepath.Join(root, ".hidden"), "hidden", "skill hidden")
	writeSkill(t, filepath.Join(root, "node_modules"), "nm", "skill nm")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	readNative := func(path string) []os.DirEntry {
		t.Helper()
		directory, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := directory.ReadDir(-1)
		if closeErr := directory.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatal(err)
		}
		return entries
	}
	var want []string
	for _, entry := range readNative(root) {
		switch entry.Name() {
		case "a":
			want = append(want, "a")
		case "b":
			for _, child := range readNative(filepath.Join(root, "b")) {
				if child.Name() == "z" {
					want = append(want, "z")
				}
			}
		case "c.md":
			want = append(want, "c")
		}
	}
	result := LoadSkillsFromDir(root)
	if got := strings.Join(skillNames(result), ","); got != strings.Join(want, ",") {
		t.Fatalf("skills = %q, want native depth-first order %q", got, strings.Join(want, ","))
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
}

// Reference: pi v1.4.0 (4a080252), core/skills.ts uses readdirSync's native
// directory order; Go's os.ReadDir sorts, so use File.ReadDir for parity.
func TestLoadSkillsFromDirPreservesNativeEntryOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "mu"} {
		writeSkill(t, root, name, "skill "+name)
	}

	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	var want []string
	for _, entry := range entries {
		if entry.IsDir() {
			want = append(want, entry.Name())
		}
	}
	result := LoadSkillsFromDir(root)
	if got := strings.Join(skillNames(result), ","); got != strings.Join(want, ",") {
		t.Fatalf("skills = %q, want native directory order %q", got, strings.Join(want, ","))
	}
}

// core/skills.ts passes includeRootFiles=false when descending into a skill
// collection; ordinary Markdown files in nested directories are not skills.
func TestLoadSkillsFromDirDoesNotLoadNestedMarkdownFiles(t *testing.T) {
	root := t.TempDir()
	writeMarkdownSkill(t, filepath.Join(root, "nested"), "notes.md", "notes", "not a discovered skill")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 0 {
		t.Fatalf("skills = %+v, want nested Markdown file ignored", result.Skills)
	}
}

// core/skills.ts addIgnoreRules applies .gitignore, .ignore, and .fdignore
// patterns before visiting skill files.
func TestLoadSkillsFromDirRespectsGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "ignored", "Must not load")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 0 {
		t.Fatalf("skills = %+v, want ignored skill omitted", result.Skills)
	}
}

// node-ignore matches JS UTF-16 units: one `?` does not consume an astral code point.
func TestLoadSkillsFromDirIgnoreQuestionMarkUsesUTF16Units(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("?/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "😀", "Should load")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 1 || result.Skills[0].Name != "😀" {
		t.Fatalf("skills = %+v, want astral directory not matched by one question mark", result.Skills)
	}
}

// node-ignore follows gitignore's rule that only a path-segment `**` is a globstar.
func TestLoadSkillsFromDirDoesNotTreatEmbeddedDoubleStarAsGlobstar(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("a**/b/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(root, "a", "x"), "b", "Should load")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 1 || result.Skills[0].Name != "b" {
		t.Fatalf("skills = %+v, want embedded ** not to cross path separators", result.Skills)
	}
}

// node-ignore's regex matching requires two `?` code units for an astral code point.
func TestLoadSkillsFromDirTwoQuestionMarksMatchAstralCodePoint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("??/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "😀", "Must not load")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 0 {
		t.Fatalf("skills = %+v, want two question marks to match UTF-16 pair", result.Skills)
	}
}

// core/skills.ts uses ignore() with node-ignore's default ignoreCase=true.
func TestLoadSkillsFromDirIgnoreRulesAreCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "IGNORED", "Must not load")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 0 {
		t.Fatalf("skills = %+v, want case-insensitive ignored match", result.Skills)
	}
}

// core/skills.ts uses the `ignore` package, which supports Gitignore glob classes.
func TestLoadSkillsFromDirSupportsIgnoreCharacterClasses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored[0-9]/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "ignored3", "Must not load")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 0 {
		t.Fatalf("skills = %+v, want character-class match ignored", result.Skills)
	}
}

// core/skills.ts prefixes each nested ignore file's patterns with its root-relative directory.
func TestLoadSkillsFromDirUsesNestedIgnoreRules(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".ignore"), []byte("blocked/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, nested, "blocked", "Must not load")

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 0 {
		t.Fatalf("skills = %+v, want nested ignored skill omitted", result.Skills)
	}
}

// core/skills.ts loadSkillsFromDirInternal follows directory symlinks with statSync.
func TestLoadSkillsFromDirFollowsDirectorySymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation may require elevated privileges")
	}
	root := t.TempDir()
	target := t.TempDir()
	writeSkill(t, target, "linked-skill", "Discovered through symlink")
	link := filepath.Join(root, "linked")
	if err := os.Symlink(filepath.Join(target, "linked-skill"), link); err != nil {
		t.Fatal(err)
	}

	result := LoadSkillsFromDir(root)
	if len(result.Skills) != 1 || result.Skills[0].Name != "linked-skill" {
		t.Fatalf("skills = %+v, want linked-skill discovered through directory symlink", result.Skills)
	}
}

// TestLoadSkillsFromDirRootSkillShortCircuits pins the one-skill-per-root rule:
// a root SKILL.md returns just that skill and the walk does not descend.
func TestLoadSkillsFromDirRootSkillShortCircuits(t *testing.T) {
	root := t.TempDir()
	content := "---\nname: top\ndescription: top skill\n---\nBody\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "child", "child skill")

	result := LoadSkillsFromDir(root)
	if got := strings.Join(skillNames(result), ","); got != "top" {
		t.Fatalf("skills = %q, want top only", got)
	}
}

// TestLoadSkillsFromDirConcurrentWalksAgree runs the bounded walk from several
// goroutines: it must be race-free and deterministic (the CI race detector
// guards the first half, this test the second).
func TestLoadSkillsFromDirConcurrentWalksAgree(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "skill a")
	writeMarkdownSkill(t, filepath.Join(root, "b"), "a.md", "x", "skill x")
	writeSkill(t, filepath.Join(root, "b"), "z", "sub")
	writeMarkdownSkill(t, root, "c.md", "c", "skill c")

	const walks = 8
	results := make([]LoadSkillsResult, walks)
	var wait sync.WaitGroup
	for index := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index] = LoadSkillsFromDir(root)
		}(index)
	}
	wait.Wait()

	want := strings.Join(skillNames(results[0]), ",")
	for index, result := range results {
		if got := strings.Join(skillNames(result), ","); got != want {
			t.Fatalf("walk %d = %q, want %q", index, got, want)
		}
	}
}

// The workspace's .agents/skills is a discovery location beside .pi/skills
// (upstream collectAncestorAgentsSkillDirs plus the resource loader's
// project-trust gate).
func TestLoadSkillsLoadsProjectAgentsSkillsWhenTrusted(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	agentDir := filepath.Join(home, ".pi", "agent")

	cwd := t.TempDir()
	writeSkill(t, filepath.Join(cwd, ".agents", "skills"), "project-agents", "project agents")

	untrusted := LoadSkills(LoadSkillsOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true}, false)
	if strings.Contains(strings.Join(skillNames(untrusted), ","), "project-agents") {
		t.Fatalf("an untrusted project's .agents skill was loaded: %v", untrusted.Skills)
	}

	trusted := LoadSkills(LoadSkillsOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true}, true)
	found := false
	for _, skill := range trusted.Skills {
		if skill.Name == "project-agents" {
			found = true
			if skill.SourceInfo.Scope != SourceScopeProject {
				t.Fatalf("scope = %q, want project", skill.SourceInfo.Scope)
			}
		}
	}
	if !found {
		t.Fatalf("a trusted project's .agents skill was not loaded: %v", trusted.Skills)
	}
}

// ~/.agents/skills is a user-level discovery location, always on when
// discovery is enabled (upstream collectResources adds it outside the trust
// gate).
func TestLoadSkillsLoadsUserAgentsSkills(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	agentDir := filepath.Join(home, ".pi", "agent")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "user-agents", "user agents")

	for _, trusted := range []bool{false, true} {
		result := LoadSkills(LoadSkillsOptions{Cwd: t.TempDir(), AgentDir: agentDir, IncludeDefaults: true}, trusted)
		var found *Skill
		for index := range result.Skills {
			if result.Skills[index].Name == "user-agents" {
				found = &result.Skills[index]
			}
		}
		if found == nil {
			t.Fatalf("trusted=%v: ~/.agents skill not loaded: %v", trusted, result.Skills)
		}
		if found.SourceInfo.Scope != SourceScopeUser {
			t.Fatalf("scope = %q, want user", found.SourceInfo.Scope)
		}
	}
}

// The project .agents walk stops at the git repository root: a .agents/skills
// above it belongs to another checkout and must not load (upstream
// collectAncestorAgentsSkillDirs).
func TestLoadSkillsAgentsAncestorsStopAtGitRoot(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	agentDir := filepath.Join(home, ".pi", "agent")

	outer := t.TempDir()
	writeSkill(t, filepath.Join(outer, ".agents", "skills"), "outer-skill", "outer")
	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(repo, ".agents", "skills"), "repo-skill", "repo")
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(sub, ".agents", "skills"), "sub-skill", "sub")

	result := LoadSkills(LoadSkillsOptions{Cwd: sub, AgentDir: agentDir, IncludeDefaults: true}, true)
	names := strings.Join(skillNames(result), ",")
	for _, want := range []string{"sub-skill", "repo-skill"} {
		if !strings.Contains(names, want) {
			t.Fatalf("skills = %q, missing %q", names, want)
		}
	}
	if strings.Contains(names, "outer-skill") {
		t.Fatalf("skills = %q, loaded a .agents dir above the git root", names)
	}
}

// When cwd is under $HOME, the ancestor walk reaches ~/.agents/skills; the
// user dir is filtered out of the project set so it loads exactly once
// (upstream filters resolve(dir) !== resolve(userAgentsSkillsDir)).
func TestLoadSkillsUserAgentsDirLoadsOnceUnderHome(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	agentDir := filepath.Join(home, ".pi", "agent")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "user-agents", "user agents")

	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	result := LoadSkills(LoadSkillsOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true}, true)
	count := 0
	for _, skill := range result.Skills {
		if skill.Name == "user-agents" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("user-agents loaded %d times, want 1: %v", count, result.Skills)
	}
}
