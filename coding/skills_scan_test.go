package coding

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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

// skillNames lists the names in order.
func skillNames(result LoadSkillsResult) []string {
	names := make([]string, 0, len(result.Skills))
	for _, skill := range result.Skills {
		names = append(names, skill.Name)
	}
	return names
}

// TestLoadSkillsFromDirVisitsEntriesInNameOrder pins the order the bounded walk
// must produce: the reference calls readdirSync, whose libuv scandir sorts entry
// names, so a directory's skills and children are visited in name order even
// though Go's File.ReadDir returns the filesystem's native order. Root SKILL.md
// files short-circuit their directory, and nested ordinary Markdown files,
// dotfiles, node_modules and non-markdown files stay skipped.
func TestLoadSkillsFromDirVisitsEntriesInNameOrder(t *testing.T) {
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

	// Entries sort as .hidden, a, b, c.md, node_modules, notes.txt; only a, b/z
	// and c.md are skills.
	want := []string{"a", "z", "c"}
	result := LoadSkillsFromDir(root)
	if got := strings.Join(skillNames(result), ","); got != strings.Join(want, ",") {
		t.Fatalf("skills = %q, want name-ordered depth-first %q", got, strings.Join(want, ","))
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
}

// Reference: pi v1.5.0 (f9a26365), core/skills.ts calls readdirSync; libuv's
// uv_fs_scandir sorts names, so discovered skills come back in name order
// regardless of creation order.
func TestLoadSkillsFromDirSortsEntriesByName(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "mu"} {
		writeSkill(t, root, name, "skill "+name)
	}
	result := LoadSkillsFromDir(root)
	if got, want := strings.Join(skillNames(result), ","), "alpha,mu,zeta"; got != want {
		t.Fatalf("skills = %q, want %q", got, want)
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

// pi v1.4.0 core/skills.ts recursively walks directories synchronously; the Go
// port preserves traversal results while bounding its parallel directory scans.
func TestLoadSkillsFromDirBoundsWorkerGoroutines(t *testing.T) {
	const childEnv = "PIER_TEST_SKILL_SCAN_GOROUTINE_CHILD"
	if os.Getenv(childEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoadSkillsFromDirBoundsWorkerGoroutines$")
		command.Env = append(os.Environ(), childEnv+"=1")
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("isolated skill-scan probe timed out: %s", output)
		}
		if err != nil {
			t.Fatalf("isolated skill-scan probe failed: %v\n%s", err, output)
		}
		return
	}

	root := t.TempDir()
	const directories = 4096
	for index := range directories {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("dir-%04d", index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan struct{})
	go func() {
		LoadSkillsFromDir(root)
		close(done)
	}()
	peak := runtime.NumGoroutine()
	ticker := time.NewTicker(100 * time.Microsecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			if peak > 128 {
				t.Fatalf("scanning %d directories peaked at %d goroutines, want at most 128", directories, peak)
			}
			return
		case <-ticker.C:
			if count := runtime.NumGoroutine(); count > peak {
				peak = count
			}
		}
	}
}
