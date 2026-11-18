package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

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
// yaml parses the frontmatter block upstream, so a comment line is not a key
// and does not become frontmatter content.
func TestParseFrontmatterSkipsCommentLines(t *testing.T) {
	parsed := ParseFrontmatter("---\n# a comment\nname: skill\n# another: not a key\ndescription: d\n---\nbody")
	if parsed.Frontmatter["name"] != "skill" || parsed.Frontmatter["description"] != "d" {
		t.Fatalf("frontmatter = %v", parsed.Frontmatter)
	}
	if _, ok := parsed.Frontmatter["# a comment"]; ok {
		t.Fatalf("a comment line became a key: %v", parsed.Frontmatter)
	}
	for key := range parsed.Frontmatter {
		if strings.HasPrefix(key, "#") {
			t.Fatalf("comment line %q became frontmatter", key)
		}
	}
}

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

// pi v1.5.2 b4afdddf utils/frontmatter.ts parses the block with the yaml
// package, so a key indented under another key belongs to that nested mapping.
// core/skills.ts reads only top-level frontmatter, so a nested description (or
// name, or disable-model-invocation) must not reach the skill: a SKILL.md whose
// only description is nested is dropped with "description is required".
func TestLoadSkillIgnoresNestedFrontmatterKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nmetadata:\n  description: nested text\n  name: other\n  disable-model-invocation: true\n---\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, diagnostics := loadSkillFromFile(path, "user")
	if skill != nil {
		t.Fatalf("skill = %+v, want the nested description ignored", skill)
	}
	if len(diagnostics) != 1 || diagnostics[0].Message != "description is required" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

// pi v1.5.2 b4afdddf core/skills.ts loadSkills passes a single-file skill's
// diagnostics through addSkills (`addSkills({ skills: [result.skill],
// diagnostics: result.diagnostics })`), so the name-validation warnings of an
// explicit skill path are reported. The port dropped them when the skill loaded.
func TestLoadSkillsKeepsAnExplicitSkillFilesDiagnostics(t *testing.T) {
	root := t.TempDir()
	setHomeDir(t, filepath.Join(root, "home"))
	skillPath := filepath.Join(root, "inline.md")
	content := "---\nname: Bad_Name\ndescription: d\n---\nBody\n"
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result := LoadSkills(LoadSkillsOptions{
		Cwd: root, AgentDir: filepath.Join(root, "agent"), SkillPaths: []string{skillPath},
	}, false)
	if len(result.Skills) != 1 || result.Skills[0].Name != "Bad_Name" {
		t.Fatalf("skills = %+v", result.Skills)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Message == "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)" {
			return
		}
	}
	t.Fatalf("diagnostics = %+v, want the name warning", result.Diagnostics)
}

// Reference: pi v1.5.0 (f9a26365), core/skills.ts formatSkillsForPrompt.
// Regenerate the golden with
// `node --experimental-strip-types coding/testdata/skills-prompt/generate.mjs ./pi`.
// The fixture also carries a skill with disable-model-invocation, which is
// listed by discovery but hidden from the prompt.
func TestFormattedSkillsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/skills-prompt/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Skills []struct {
			Name                   string `json:"name"`
			Description            string `json:"description"`
			DisableModelInvocation bool   `json:"disableModelInvocation"`
		} `json:"skills"`
		Prompts map[string]string `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.Abs(filepath.Join("testdata", "skills-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	result := LoadSkillsFromDir(dir)
	if len(result.Skills) != len(golden.Skills) {
		t.Fatalf("skills = %d, want %d", len(result.Skills), len(golden.Skills))
	}
	for index, skill := range result.Skills {
		want := golden.Skills[index]
		if skill.Name != want.Name || skill.Description != want.Description || skill.DisableModelInvocation != want.DisableModelInvocation {
			t.Errorf("skill %d = %q/%q/%v, want %q/%q/%v", index,
				skill.Name, skill.Description, skill.DisableModelInvocation,
				want.Name, want.Description, want.DisableModelInvocation)
		}
	}
	for _, tool := range []string{"read", "bash", "indirect"} {
		got := FormatSkillsForPrompt(result.Skills, tool)
		for _, absolute := range []string{dir, filepath.ToSlash(dir)} {
			got = strings.ReplaceAll(got, absolute, "<dir>")
		}
		if got != golden.Prompts[tool] {
			t.Errorf("prompt for %s differs:\n got %q\nwant %q", tool, got, golden.Prompts[tool])
		}
	}
}

// pi v1.4.0 core/skills.ts loadSkillFromFile validates both description and
// name before rejecting a declared skill whose description is empty.
func TestMissingSkillDescriptionStillReportsInvalidName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Bad--Name\ndescription: \"\"\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := LoadSkillsFromDir(dir)
	var messages []string
	for _, diagnostic := range result.Diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	want := []string{"description is required", "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)", "name must not contain consecutive hyphens"}
	if len(result.Skills) != 0 || !reflect.DeepEqual(messages, want) {
		t.Fatalf("skills=%+v diagnostics=%q, want no skill and diagnostics=%q", result.Skills, messages, want)
	}
}
