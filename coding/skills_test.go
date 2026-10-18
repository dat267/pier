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

// TestLoadSkillsDedupesDefaultsAndPaths covers upstream loadSkills' real-path
// dedupe: the default agent-dir skills dir and a settings skill path pointing
// at the same directory (e.g. ~/.pi/agent/skills) must load each skill once,
// with no diagnostic. The port appended both, so every skill appeared twice.
// skills.ts reads skill files as UTF-8 and replaces malformed sequences.
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
// dotfiles, node_modules and non-markdown files stay skipped.
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

	result := LoadSkillsFromDir(root)
	if got, want := strings.Join(skillNames(result), ","), "a,x,z,c"; got != want {
		t.Fatalf("skills = %q, want %q", got, want)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
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
