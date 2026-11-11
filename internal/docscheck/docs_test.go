package docscheck

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// root is the repository root, relative to this package directory.
const root = "../.."

// docs are the root documents whose cross-references these tests check.
var docs = []string{"AGENTS.md", "ARCHITECTURE.md", "DECISIONS.md", "README.md", "TODO.md"}

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

var (
	dRowHeading = regexp.MustCompile(`(?m)^### (D\d+(?:/D\d+)*)(?: \([a-z ]+\))?\s*$`)
	anyHeading  = regexp.MustCompile(`(?m)^#{1,3} `)
	dRowTitle   = regexp.MustCompile(`(?m)\A# Deliberate divergences from pi \(D1–D(\d+)\)\n`)
)

// TestDecisionRowsAreUniqueAndOrdered pins the DECISIONS.md contract: rows are
// numbered once, appear in ascending order (a new row is inserted, not
// appended, when it reuses a lower number), and the title range names the
// highest row. Before this check the range sentence had drifted 15 rows behind
// the title and 20 rows were out of order.
func TestDecisionRowsAreUniqueAndOrdered(t *testing.T) {
	text := readDoc(t, "DECISIONS.md")
	title := dRowTitle.FindStringSubmatch(text)
	if title == nil {
		t.Fatal("DECISIONS.md must start with `# Deliberate divergences from pi (D1–Dn)`")
	}
	seen := map[int]string{}
	prev, prevHeading, highest := 0, "", 0
	for _, m := range dRowHeading.FindAllStringSubmatch(text, -1) {
		ids := strings.Split(m[1], "/")
		for _, id := range ids {
			n, _ := strconv.Atoi(id[1:])
			if other, dup := seen[n]; dup {
				t.Errorf("D%d appears in both %q and %q", n, other, m[1])
			}
			seen[n] = m[1]
			highest = max(highest, n)
		}
		first, _ := strconv.Atoi(ids[0][1:])
		if first <= prev {
			t.Errorf("%q follows %q; keep rows in ascending order", m[1], prevHeading)
		}
		last, _ := strconv.Atoi(ids[len(ids)-1][1:])
		prev, prevHeading = last, m[1]
	}
	if len(seen) == 0 {
		t.Fatal("DECISIONS.md has no `### Dn` rows")
	}
	if want := strconv.Itoa(highest); title[1] != want {
		t.Errorf("title range ends at D%s, but the highest row is D%s", title[1], want)
	}
}

var (
	// headingLike matches every `###` heading, including the ones the row
	// pattern rejects, so a malformed row is reported instead of silently
	// escaping the row checks.
	headingLike = regexp.MustCompile(`(?m)^### .*$`)

	namedTest = regexp.MustCompile("`((?:Test|Benchmark|Fuzz)[A-Za-z0-9_]+)")
	testFunc  = regexp.MustCompile(`(?m)^func ((?:Test|Benchmark|Fuzz)[A-Za-z0-9_]+)\(`)
)

// citeDRows records every whole-word `D<digits>` in b. It replaces the regexp
// `\bD(\d+)\b`, which took most of this package's run over the generated data.
func citeDRows(b []byte, cited map[int]bool) {
	word := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	for i := bytes.IndexByte(b, 'D'); i >= 0; {
		j := i + 1
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			j++
		}
		if j > i+1 && (i == 0 || !word(b[i-1])) && (j == len(b) || !word(b[j])) {
			n, _ := strconv.Atoi(string(b[i+1 : j]))
			cited[n] = true
		}
		next := bytes.IndexByte(b[j:], 'D')
		if next < 0 {
			break
		}
		i = j + next
	}
}

// goSources scans the module's Go files once per test binary: the test
// functions defined in _test.go files and the D-row numbers any Go comment or
// string cites.
func goSources(t *testing.T) (tests map[string]bool, cited map[int]bool) {
	t.Helper()
	scan := scanGoSources()
	if scan.err != nil {
		t.Fatal(scan.err)
	}
	return scan.tests, scan.cited
}

type goScan struct {
	tests map[string]bool
	cited map[int]bool
	err   error
}

var scanGoSources = sync.OnceValue(func() goScan {
	scan := goScan{tests: map[string]bool{}, cited: map[int]bool{}}
	scan.err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "pi", "tmp", "bin", "testdata", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			for _, m := range testFunc.FindAllSubmatch(b, -1) {
				scan.tests[string(m[1])] = true
			}
		}
		// This package's own fixtures spell out D-numbers on purpose, so they
		// are not citations of a row.
		if filepath.Dir(path) != filepath.Join(root, "internal", "docscheck") {
			citeDRows(b, scan.cited)
		}
		return nil
	})
	return scan
})

// TestEveryDecisionHeadingIsARow fails when a `###` heading in DECISIONS.md is
// not a row the other checks parse. A heading outside the shape
// (`### D12 — words`) is invisible to uniqueness, ordering, the reproduction
// rule and the citation check, and only a malformed *highest* row trips the
// title range: a row written that way can cite nothing, name no test, and break
// the order, with every other check in this package still green. That phantom
// row was reproduced before this check existed: `### D100 — a malformed row
// heading` with a placeholder body under D50 left the package passing.
func TestEveryDecisionHeadingIsARow(t *testing.T) {
	text := readDoc(t, "DECISIONS.md")
	for _, m := range headingLike.FindAllStringIndex(text, -1) {
		heading := text[m[0]:m[1]]
		if dRowHeading.FindStringIndex(heading) == nil {
			t.Errorf("%q is not a row heading; write `### D<n>` (or `### D<n>/D<m>`) with nothing after the number", heading)
		}
	}
}

// TestDocumentedTestsExist fails when a document names a test that no longer
// exists, so a rename or deletion updates the row that cites it.
func TestDocumentedTestsExist(t *testing.T) {
	defined, _ := goSources(t)
	for _, name := range docs {
		for _, m := range namedTest.FindAllStringSubmatch(readDoc(t, name), -1) {
			if !defined[m[1]] {
				t.Errorf("%s names %s, which no _test.go defines", name, m[1])
			}
		}
	}
}

var (
	localLink = regexp.MustCompile(`\]\(([A-Za-z0-9_.-]+\.md)(?:#([^)\s]+))?\)`)
	heading   = regexp.MustCompile(`(?m)^#{1,6} (.+?)\s*$`)
	slugDrop  = regexp.MustCompile(`[^\p{L}\p{N} _-]`)
)

// githubSlug mirrors GitHub's heading anchors: lowercase, punctuation removed,
// spaces to hyphens. Duplicate headings are not suffixed; no link relies on one.
func githubSlug(h string) string {
	h = strings.ToLower(strings.ReplaceAll(h, "`", ""))
	return strings.ReplaceAll(slugDrop.ReplaceAllString(h, ""), " ", "-")
}

// TestRootDocumentLinksResolve checks `[x](FILE.md#anchor)` links between the
// root documents, the references most likely to break when a section moves.
func TestRootDocumentLinksResolve(t *testing.T) {
	anchors := map[string]map[string]bool{}
	for _, name := range docs {
		set := map[string]bool{}
		for _, m := range heading.FindAllStringSubmatch(readDoc(t, name), -1) {
			set[githubSlug(m[1])] = true
		}
		anchors[name] = set
	}
	for _, name := range docs {
		for _, m := range localLink.FindAllStringSubmatch(readDoc(t, name), -1) {
			target, anchor := m[1], m[2]
			set, ok := anchors[target]
			if !ok {
				if _, err := os.Stat(filepath.Join(root, target)); err != nil {
					t.Errorf("%s links to missing %s", name, target)
				}
				continue
			}
			if anchor != "" && !set[anchor] {
				t.Errorf("%s links to %s#%s, which has no such heading", name, target, anchor)
			}
		}
	}
}

var agentsPin = regexp.MustCompile("upstream implementation \\((v[0-9.]+), `([0-9a-f]+)`\\)")

// TestDocumentsNameTheAgentsPin keeps the pin in README.md and ARCHITECTURE.md
// in step with AGENTS.md, which `just reference` reads as the source of truth.
func TestDocumentsNameTheAgentsPin(t *testing.T) {
	m := agentsPin.FindStringSubmatch(readDoc(t, "AGENTS.md"))
	if m == nil {
		t.Fatal("AGENTS.md has no `upstream implementation (vX.Y.Z, `sha`)` pin")
	}
	want := regexp.MustCompile(regexp.QuoteMeta(m[1]) + "\\s+\\(`" + m[2] + "`\\)")
	for _, name := range []string{"README.md", "ARCHITECTURE.md"} {
		if !want.MatchString(readDoc(t, name)) {
			t.Errorf("%s does not name the AGENTS.md pin %s (`%s`)", name, m[1], m[2])
		}
	}
}

// TestDecisionRowsAreReproduced enforces the AGENTS.md rule that a deliberate
// difference has a reproducing test: each DECISIONS.md row either names a test
// that exists or is cited by its number from Go source next to the behavior.
func TestDecisionRowsAreReproduced(t *testing.T) {
	defined, cited := goSources(t)
	text := readDoc(t, "DECISIONS.md")
	matches := dRowHeading.FindAllStringSubmatchIndex(text, -1)
	for i, m := range matches {
		bodyEnd := len(text)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		body := text[m[1]:bodyEnd]
		if next := anyHeading.FindStringIndex(body); next != nil {
			body = body[:next[0]]
		}
		ids := text[m[2]:m[3]]
		reproduced := false
		for _, n := range namedTest.FindAllStringSubmatch(body, -1) {
			reproduced = reproduced || defined[n[1]]
		}
		for _, id := range strings.Split(ids, "/") {
			n, _ := strconv.Atoi(id[1:])
			reproduced = reproduced || cited[n]
		}
		if !reproduced {
			t.Errorf("%s names no existing test and no Go source cites it", ids)
		}
	}
}

func TestCiteDRowsMatchesWholeWords(t *testing.T) {
	got := map[int]bool{}
	citeDRows([]byte("D1 xD2 D3x D44/D47 (D5) D_6 D"), got)
	want := map[int]bool{1: true, 44: true, 47: true, 5: true}
	if len(got) != len(want) {
		t.Fatalf("cited = %v, want %v", got, want)
	}
	for n := range want {
		if !got[n] {
			t.Errorf("D%d not cited; got %v", n, got)
		}
	}
}

// TestEveryCitedDRowHasAnEntry keeps the citation contract the title of
// DECISIONS.md promises: a Go comment that cites `D<n>` points at a row that
// explains it. Thirty-eight such numbers predated this file and were silently
// dangling, so a citation could name nothing and still pass every other check.
func TestEveryCitedDRowHasAnEntry(t *testing.T) {
	_, cited := goSources(t)
	text := readDoc(t, "DECISIONS.md")
	defined := map[int]bool{}
	for _, m := range dRowHeading.FindAllStringSubmatch(text, -1) {
		for _, id := range strings.Split(m[1], "/") {
			n, err := strconv.Atoi(id[1:])
			if err != nil {
				t.Fatalf("bad row id %q", id)
			}
			defined[n] = true
		}
	}
	var missing []string
	for n := range cited {
		if !defined[n] {
			missing = append(missing, strconv.Itoa(n))
		}
	}
	if len(missing) > 0 {
		sort.Slice(missing, func(i, j int) bool {
			a, _ := strconv.Atoi(missing[i])
			b, _ := strconv.Atoi(missing[j])
			return a < b
		})
		t.Errorf("Go source cites %d D-row numbers with no DECISIONS.md entry: D%s",
			len(missing), strings.Join(missing, ", D"))
	}
}
