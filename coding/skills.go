package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dat267/pier/ai"
)

// Port of utils/frontmatter.ts (the YAML subset skill files use) and
// core/skills.ts.

// D-row D10: upstream parses YAML with the `yaml` package; the Go port
// implements the flat `key: value` subset the Agent Skills spec's
// frontmatter uses. It tracks quoted versus plain scalar types and common
// boolean, numeric and null values, but does not support nested YAML structures.

// ParsedFrontmatter is the parse outcome.
type ParsedFrontmatter struct {
	Frontmatter map[string]string
	Booleans    map[string]bool
	NonStrings  map[string]bool
	Body        string
}

var bomRune = "\uFEFF"

// ParseFrontmatter splits and parses the skill file's frontmatter.
func ParseFrontmatter(content string) ParsedFrontmatter {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimPrefix(normalized, bomRune)

	result := ParsedFrontmatter{
		Frontmatter: map[string]string{},
		Booleans:    map[string]bool{},
		NonStrings:  map[string]bool{},
	}
	if !strings.HasPrefix(normalized, "---") {
		result.Body = normalized
		return result
	}
	endIndex := strings.Index(normalized[3:], "\n---")
	if endIndex == -1 {
		result.Body = normalized
		return result
	}
	yamlString := normalized[4 : 3+endIndex+1]
	body := ai.JSTrim(normalized[3+endIndex+4:])

	for _, line := range strings.Split(yamlString, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		if commentIndex := yamlInlineCommentIndex(value); commentIndex >= 0 {
			value = strings.TrimSpace(value[:commentIndex])
		}
		quoted := len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'')
		if quoted {
			value = value[1 : len(value)-1]
		}
		result.Frontmatter[key] = value
		if !quoted && !yamlPlainScalarIsString(value) {
			result.NonStrings[key] = true
		}
		if !quoted && strings.EqualFold(value, "true") {
			result.Booleans[key] = true
		}
	}
	result.Body = body
	return result
}

func yamlPlainScalarIsString(value string) bool {
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "true", "false", "null", "~", ".nan", ".inf", "+.inf", "-.inf":
		return false
	case "nan", "+nan", "-nan", "inf", "+inf", "-inf", "infinity", "+infinity", "-infinity":
		return true
	}
	if _, err := strconv.ParseInt(value, 0, 64); err == nil {
		return false
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return false
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
		return false
	}
	return true
}

func yamlInlineCommentIndex(value string) int {
	var quote byte
	for index := 0; index < len(value); index++ {
		character := value[index]
		if quote == '"' && character == '\\' {
			index++
			continue
		}
		if quote == '\'' && character == '\'' && index+1 < len(value) && value[index+1] == '\'' {
			index++
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if index == 0 && (character == '"' || character == '\'') {
			quote = character
			continue
		}
		if character == '#' && (index == 0 || value[index-1] == ' ' || value[index-1] == '\t') {
			return index
		}
	}
	return -1
}

// StripFrontmatter returns the body only.
func StripFrontmatter(content string) string {
	return ParseFrontmatter(content).Body
}

// Skill is one discovered skill.
type Skill struct {
	Name        string
	Description string
	FilePath    string
	BaseDir     string
	// SourceInfo describes where the skill came from (upstream sourceInfo).
	SourceInfo SourceInfo
	// DisableModelInvocation hides the skill from the model prompt.
	DisableModelInvocation bool
}

// createSkillSourceInfo maps a loader source to its SourceInfo (upstream
// createSkillSourceInfo).
func createSkillSourceInfo(filePath string, baseDir string, source string) SourceInfo {
	switch source {
	case "user":
		return CreateSyntheticSourceInfo(filePath, "local", SourceScopeUser, SourceOriginTopLevel, baseDir)
	case "project":
		return CreateSyntheticSourceInfo(filePath, "local", SourceScopeProject, SourceOriginTopLevel, baseDir)
	case "path":
		return CreateSyntheticSourceInfo(filePath, "local", SourceScopeTemporary, SourceOriginTopLevel, baseDir)
	default:
		return CreateSyntheticSourceInfo(filePath, source, SourceScopeTemporary, SourceOriginTopLevel, baseDir)
	}
}

// ResourceCollision records which resource won a name collision (upstream
// ResourceDiagnostic.collision).
type ResourceCollision struct {
	ResourceType string
	Name         string
	WinnerPath   string
	LoserPath    string
}

// ResourceDiagnostic is a skill validation warning or name collision.
type ResourceDiagnostic struct {
	Type      string
	Message   string
	Path      string
	Collision *ResourceCollision
}

// LoadSkillsResult bundles skills and diagnostics.
type LoadSkillsResult struct {
	Skills      []Skill
	Diagnostics []ResourceDiagnostic
}

const (
	maxSkillNameLength        = 64
	maxSkillDescriptionLength = 1024
)

// validateSkillName validates per the Agent Skills spec.
// skillNameRegex matches a valid skill name: lowercase letters, digits and
// hyphens.
var skillNameRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

func validateSkillName(name string) []string {
	var errors []string
	nameLength := ai.JSLength(name)
	if nameLength > maxSkillNameLength {
		errors = append(errors, fmt.Sprintf("name exceeds %d characters (%d)", maxSkillNameLength, nameLength))
	}
	if !skillNameRegex.MatchString(name) {
		errors = append(errors, "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errors = append(errors, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errors = append(errors, "name must not contain consecutive hyphens")
	}
	return errors
}

// loadSkillFromFile parses one skill file.
func loadSkillFromFile(filePath string, source string) (*Skill, []ResourceDiagnostic) {
	var diagnostics []ResourceDiagnostic
	isDeclaredSkill := filepath.Base(filePath) == "SKILL.md"

	rawContent, err := os.ReadFile(filePath)
	if err != nil {
		diagnostics = append(diagnostics, ResourceDiagnostic{
			Type: "warning", Message: err.Error(), Path: filePath,
		})
		return nil, diagnostics
	}

	parsed := ParseFrontmatter(DecodeUTF8WithReplacement(rawContent))
	description := parsed.Frontmatter["description"]
	if parsed.NonStrings["description"] {
		description = ""
	}
	hasDescription := ai.JSTrim(description) != ""
	if !isDeclaredSkill && !hasDescription {
		return nil, diagnostics
	}

	skillDir := filepath.Dir(filePath)
	parentDirName := filepath.Base(skillDir)

	// Missing/empty description drops the skill (after validation warnings).
	if !hasDescription {
		if isDeclaredSkill {
			diagnostics = append(diagnostics, ResourceDiagnostic{
				Type: "warning", Message: "description is required", Path: filePath,
			})
		}
		return nil, diagnostics
	}

	descriptionLength := ai.JSLength(description)
	if descriptionLength > maxSkillDescriptionLength {
		diagnostics = append(diagnostics, ResourceDiagnostic{Type: "warning", Message: fmt.Sprintf("description exceeds %d characters (%d)", maxSkillDescriptionLength, descriptionLength), Path: filePath})
	}

	name := parsed.Frontmatter["name"]
	if parsed.NonStrings["name"] {
		name = ""
	}
	if name == "" {
		name = parentDirName
	}
	for _, err := range validateSkillName(name) {
		diagnostics = append(diagnostics, ResourceDiagnostic{Type: "warning", Message: err, Path: filePath})
	}

	return &Skill{
		Name:                   name,
		Description:            description,
		FilePath:               filePath,
		BaseDir:                skillDir,
		SourceInfo:             createSkillSourceInfo(filePath, skillDir, source),
		DisableModelInvocation: parsed.Booleans["disable-model-invocation"],
	}, diagnostics
}

// LoadSkillsFromDir scans a directory tree: a root SKILL.md wins, then
// subdirectories are scanned recursively; dotfiles and node_modules are
// skipped (port of loadSkillsFromDir). The source identifier defaults to the
// temporary scope.
func LoadSkillsFromDir(dir string) LoadSkillsResult {
	return loadSkillsFromDir(dir, "")
}

// skillScanConcurrency bounds the directory reads one skills-tree walk keeps
// in flight. A skills tree is one of four concurrent boot passes (see
// CreateAgentSession's resource discovery), so this also keeps a deep tree
// from flooding the process with filesystem work.
const skillScanConcurrency = 4

// skillScanDir is one directory's scan: either a root SKILL.md short-circuit
// (terminal), or its entries in ReadDir order.
type skillScanDir struct {
	terminal bool
	root     *Skill
	rootDiag []ResourceDiagnostic
	items    []skillScanItem
	rules    []skillIgnoreRule
}

// skillScanItem is one entry in ReadDir order: a subdirectory (scanned later)
// or a parsed skill file.
type skillScanItem struct {
	subdir string
	skill  *Skill
	diag   []ResourceDiagnostic
}

// loadSkillsFromDir is LoadSkillsFromDir with the loader source identifier.
func loadSkillsFromDir(dir string, source string) LoadSkillsResult {
	scans := scanSkillTree(dir, source)
	if scans[dir] == nil {
		return LoadSkillsResult{}
	}
	var skills []Skill
	var diagnostics []ResourceDiagnostic
	assembleSkillDir(dir, scans, &skills, &diagnostics)
	return LoadSkillsResult{Skills: skills, Diagnostics: diagnostics}
}

// scanSkillTree reads every directory under rootDir on a bounded pool. The
// listings are independent, so on a high-latency filesystem the walk costs the
// slowest branch rather than the sum of every branch. The scan map is local to
// the call, so concurrent walks (and the concurrent boot passes that call this)
// share no state.
func scanSkillTree(rootDir string, source string) map[string]*skillScanDir {
	scans := map[string]*skillScanDir{}
	var mu sync.Mutex
	var wait sync.WaitGroup
	sem := make(chan struct{}, skillScanConcurrency)
	var walk func(dir string, includeRootFiles bool, inheritedRules []skillIgnoreRule)
	walk = func(dir string, includeRootFiles bool, inheritedRules []skillIgnoreRule) {
		// Add happens while the parent's count is still held, so it cannot race
		// the Wait() below (the standard WaitGroup reuse rule).
		wait.Add(1)
		go func() {
			defer wait.Done()
			sem <- struct{}{}
			scan := readSkillDir(dir, rootDir, source, includeRootFiles, inheritedRules)
			<-sem
			mu.Lock()
			scans[dir] = scan
			mu.Unlock()
			// Children are queued after the slot is released and before this
			// goroutine returns, so a blocked child cannot deadlock the pool.
			for _, item := range scan.items {
				if item.subdir != "" {
					walk(item.subdir, false, scan.rules)
				}
			}
		}()
	}
	walk(rootDir, true, nil)
	wait.Wait()
	return scans
}

// skillEntryType follows symlinks like upstream's statSync checks. Broken links
// are skipped.
type skillIgnoreRule struct {
	pattern       *regexp.Regexp
	negated       bool
	directoryOnly bool
}

func skillIgnoreRulesForDir(dir, root string) []skillIgnoreRule {
	relativeDir, err := filepath.Rel(root, dir)
	if err != nil || relativeDir == "." {
		relativeDir = ""
	} else {
		relativeDir = strings.ReplaceAll(relativeDir, string(os.PathSeparator), "/") + "/"
	}
	var rules []skillIgnoreRule
	for _, filename := range []string{".gitignore", ".ignore", ".fdignore"} {
		content, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			rule, ok := parseSkillIgnoreRule(strings.TrimSuffix(line, "\r"), relativeDir)
			if ok {
				rules = append(rules, rule)
			}
		}
	}
	return rules
}

func parseSkillIgnoreRule(line, prefix string) (skillIgnoreRule, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || (strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "\\#")) {
		return skillIgnoreRule{}, false
	}
	pattern := line
	for strings.HasSuffix(pattern, " ") {
		backslashes := 0
		for index := len(pattern) - 2; index >= 0 && pattern[index] == 0x5c; index-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			break
		}
		pattern = pattern[:len(pattern)-1]
	}
	negated := false
	if strings.HasPrefix(pattern, "!") {
		negated = true
		pattern = pattern[1:]
	} else if strings.HasPrefix(pattern, "\\!") {
		pattern = pattern[1:]
	}
	if strings.HasPrefix(pattern, "\\#") {
		pattern = pattern[1:]
	}
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	directoryOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if prefix != "" {
		pattern = prefix + pattern
		anchored = true
	}
	if pattern == "" {
		return skillIgnoreRule{}, false
	}
	pattern = strings.ToLower(pattern)
	regexPattern := globToRegexp(pattern)
	if !anchored && !strings.Contains(pattern, "/") {
		regexPattern = `(?:^|/)` + regexPattern
	}
	compiled, err := regexp.Compile(`^` + regexPattern + `$`)
	if err != nil {
		return skillIgnoreRule{}, false
	}
	return skillIgnoreRule{pattern: compiled, negated: negated, directoryOnly: directoryOnly}, true
}

func encodeIgnorePath(path string) string {
	var builder strings.Builder
	for _, character := range path {
		writeIgnoreUnits(&builder, character)
	}
	return builder.String()
}

func writeIgnoreUnits(builder *strings.Builder, character rune) {
	if character <= 0xFFFF {
		builder.WriteRune(character)
		return
	}
	character -= 0x10000
	builder.WriteRune(0xF0000 + 0xD800 + (character >> 10))
	builder.WriteRune(0xF0000 + 0xDC00 + (character & 0x3FF))
}

func writeIgnoreLiteral(builder *strings.Builder, character rune) {
	if character <= 0xFFFF {
		builder.WriteString(regexp.QuoteMeta(string(character)))
		return
	}
	character -= 0x10000
	builder.WriteString(regexp.QuoteMeta(string(rune(0xF0000 + 0xD800 + (character >> 10)))))
	builder.WriteString(regexp.QuoteMeta(string(rune(0xF0000 + 0xDC00 + (character & 0x3FF)))))
}

func globToRegexp(pattern string) string {
	var builder strings.Builder
	runes := []rune(pattern)
	for index := 0; index < len(runes); index++ {
		switch runes[index] {
		case '\\':
			if index+1 < len(runes) {
				index++
			}
			writeIgnoreLiteral(&builder, runes[index])
		case '*':
			start := index
			for index+1 < len(runes) && runes[index+1] == '*' {
				index++
			}
			starCount := index - start + 1
			segmentBoundaryBefore := start == 0 || runes[start-1] == '/'
			segmentBoundaryAfter := index+1 == len(runes) || runes[index+1] == '/'
			if starCount == 2 && segmentBoundaryBefore && segmentBoundaryAfter {
				if index+1 < len(runes) && runes[index+1] == '/' {
					index++
					builder.WriteString(`(?s:.*/)?`)
				} else {
					builder.WriteString(`(?s:.*)`)
				}
			} else {
				builder.WriteString(`[^/]*`)
			}
		case '?':
			builder.WriteString(`[^/]`)
		case '[':
			end := index + 1
			for end < len(runes) {
				if runes[end] == '\\' && end+1 < len(runes) {
					end += 2
					continue
				}
				if runes[end] == ']' {
					break
				}
				end++
			}
			if end == len(runes) || end == index+1 || (end == index+2 && runes[index+1] == '!') {
				builder.WriteString(regexp.QuoteMeta("["))
				continue
			}
			class := runes[index+1 : end]
			builder.WriteByte('[')
			if class[0] == '!' {
				builder.WriteByte('^')
				class = class[1:]
			} else if class[0] == '^' {
				builder.WriteByte('\\')
			}
			builder.WriteString(string(class))
			builder.WriteByte(']')
			index = end
		default:
			writeIgnoreLiteral(&builder, runes[index])
		}
	}
	return builder.String()
}

func skillPathIgnored(path string, isDirectory bool, rules []skillIgnoreRule) bool {
	ignored := false
	for _, rule := range rules {
		if rule.directoryOnly && !isDirectory {
			continue
		}
		if rule.pattern.MatchString(encodeIgnorePath(strings.ToLower(path))) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func relativeSkillPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return strings.ReplaceAll(relative, string(os.PathSeparator), "/")
}

func skillEntryType(entry os.DirEntry, fullPath string) (isFile, isDirectory bool) {
	if entry.Type()&os.ModeSymlink != 0 {
		info, err := os.Stat(fullPath)
		if err != nil {
			return false, false
		}
		return info.Mode().IsRegular(), info.IsDir()
	}
	return entry.Type().IsRegular(), entry.IsDir()
}

// readSkillDir reads one directory and parses the skill files directly inside
// it. A subdirectory becomes an item; its scan is filled in by a later worker.
func readSkillDir(dir, root string, source string, includeRootFiles bool, inheritedRules []skillIgnoreRule) *skillScanDir {
	// ReadDir reports a missing or unreadable directory by itself; a separate
	// os.Stat would double the walk's directory operations.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return &skillScanDir{}
	}
	rules := append([]skillIgnoreRule(nil), inheritedRules...)
	rules = append(rules, skillIgnoreRulesForDir(dir, root)...)
	// A root SKILL.md short-circuits (one skill per directory root).
	for _, entry := range entries {
		name := DecodeUTF8WithReplacement([]byte(entry.Name()))
		fullPath := filepath.Join(dir, name)
		isFile, _ := skillEntryType(entry, fullPath)
		if name != "SKILL.md" || !isFile || skillPathIgnored(relativeSkillPath(root, fullPath), false, rules) {
			continue
		}
		skill, diag := loadSkillFromFile(fullPath, source)
		return &skillScanDir{terminal: true, root: skill, rootDiag: diag}
	}
	scan := &skillScanDir{rules: rules}
	for _, entry := range entries {
		name := DecodeUTF8WithReplacement([]byte(entry.Name()))
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		fullPath := filepath.Join(dir, name)
		isFile, isDirectory := skillEntryType(entry, fullPath)
		if skillPathIgnored(relativeSkillPath(root, fullPath), isDirectory, rules) {
			continue
		}
		if isDirectory {
			scan.items = append(scan.items, skillScanItem{subdir: fullPath})
			continue
		}
		if !isFile || !includeRootFiles || !strings.HasSuffix(name, ".md") {
			continue
		}
		skill, diag := loadSkillFromFile(fullPath, source)
		scan.items = append(scan.items, skillScanItem{skill: skill, diag: diag})
	}
	return scan
}

// assembleSkillDir replays one directory's scan depth-first in ReadDir order,
// which reproduces the sequential walk's skills and diagnostics order exactly.
func assembleSkillDir(dir string, scans map[string]*skillScanDir, skills *[]Skill, diagnostics *[]ResourceDiagnostic) {
	scan := scans[dir]
	if scan == nil {
		return
	}
	if scan.terminal {
		if scan.root != nil {
			*skills = append(*skills, *scan.root)
		}
		*diagnostics = append(*diagnostics, scan.rootDiag...)
		return
	}
	for _, item := range scan.items {
		if item.subdir != "" {
			assembleSkillDir(item.subdir, scans, skills, diagnostics)
			continue
		}
		if item.skill != nil {
			*skills = append(*skills, *item.skill)
		}
		*diagnostics = append(*diagnostics, item.diag...)
	}
}

// LoadSkillsOptions are LoadSkills inputs.
type LoadSkillsOptions struct {
	Cwd             string
	AgentDir        string
	SkillPaths      []string
	IncludeDefaults bool
}

// LoadSkills loads skills from user and project locations: the agent and user
// `.pi/skills` dirs, the user `~/.agents/skills` dir, and the project's
// `.agents/skills` dirs from cwd up to the git repository root. The project
// dirs are only scanned when trustProject is set (upstream's project-trust
// rule; pi answers "untrusted" headless by default).
func LoadSkills(options LoadSkillsOptions, trustProject bool) LoadSkillsResult {
	resolvedCwd := ResolvePath(options.Cwd, "", PathInputOptions{})
	resolvedAgentDir := ResolvePath(options.AgentDir, "", PathInputOptions{})

	// Insertion-ordered name map (upstream's Map preserves insertion order).
	var skills []Skill
	skillIndex := map[string]int{}
	realPathSet := map[string]bool{}
	var allDiagnostics []ResourceDiagnostic
	var collisionDiagnostics []ResourceDiagnostic

	addSkills := func(result LoadSkillsResult) {
		allDiagnostics = append(allDiagnostics, result.Diagnostics...)
		for _, skill := range result.Skills {
			// Resolve symlinks so the same file loaded twice is skipped silently.
			realPath := CanonicalizePath(skill.FilePath)
			if realPathSet[realPath] {
				continue
			}
			if existingIndex, ok := skillIndex[skill.Name]; ok {
				existing := skills[existingIndex]
				collisionDiagnostics = append(collisionDiagnostics, ResourceDiagnostic{
					Type: "collision", Message: "name \"" + skill.Name + "\" collision", Path: skill.FilePath,
					Collision: &ResourceCollision{
						ResourceType: "skill", Name: skill.Name,
						WinnerPath: existing.FilePath, LoserPath: skill.FilePath,
					},
				})
				continue
			}
			skillIndex[skill.Name] = len(skills)
			skills = append(skills, skill)
			realPathSet[realPath] = true
		}
	}

	userSkillsDir := filepath.Join(resolvedAgentDir, "skills")
	projectSkillsDir := filepath.Join(resolvedCwd, ".pi", "skills")
	isUnderPath := func(target string, root string) bool {
		normalizedRoot := filepath.Clean(root)
		if target == normalizedRoot {
			return true
		}
		prefix := normalizedRoot + string(filepath.Separator)
		return strings.HasPrefix(target, prefix)
	}

	userAgentsSkillsDir := userAgentsSkillsDirPath()
	if options.IncludeDefaults {
		addSkills(loadSkillsFromDir(userSkillsDir, "user"))
	}
	// The project skills dirs stay gated on project trust (the Go loader's
	// added parameter; upstream gates them in the resource loader).
	if trustProject {
		addSkills(loadSkillsFromDir(projectSkillsDir, "project"))
		if options.IncludeDefaults {
			// The workspace's .agents/skills from cwd up to the git repository
			// root, with the user dir filtered out so it loads once with the
			// user scope (upstream collectAncestorAgentsSkillDirs and the
			// package manager's project-trust gate).
			for _, dir := range projectAgentsSkillDirs(resolvedCwd, true, userAgentsSkillsDir) {
				addSkills(loadSkillsFromDir(dir, "project"))
			}
		}
	}
	if options.IncludeDefaults && userAgentsSkillsDir != "" {
		// ~/.agents/skills is a user-level location, on regardless of trust
		// (upstream collectResources adds it outside the trust gate).
		addSkills(loadSkillsFromDir(userAgentsSkillsDir, "user"))
	}

	getSource := func(resolvedPath string) string {
		if !options.IncludeDefaults {
			if isUnderPath(resolvedPath, userSkillsDir) {
				return "user"
			}
			if isUnderPath(resolvedPath, projectSkillsDir) {
				return "project"
			}
		}
		return "path"
	}

	for _, rawPath := range options.SkillPaths {
		resolvedPath := ResolvePath(rawPath, resolvedCwd, PathInputOptions{Trim: true})
		info, err := os.Stat(resolvedPath)
		if err != nil {
			allDiagnostics = append(allDiagnostics, ResourceDiagnostic{
				Type: "warning", Message: "skill path does not exist", Path: resolvedPath,
			})
			continue
		}
		source := getSource(resolvedPath)
		if info.IsDir() {
			addSkills(loadSkillsFromDir(resolvedPath, source))
			continue
		}
		if info.Mode().IsRegular() && strings.HasSuffix(resolvedPath, ".md") {
			skill, diag := loadSkillFromFile(resolvedPath, source)
			if skill != nil {
				addSkills(LoadSkillsResult{Skills: []Skill{*skill}})
			} else {
				allDiagnostics = append(allDiagnostics, diag...)
			}
			continue
		}
		allDiagnostics = append(allDiagnostics, ResourceDiagnostic{
			Type: "warning", Message: "skill path is not a markdown file", Path: resolvedPath,
		})
	}

	return LoadSkillsResult{Skills: skills, Diagnostics: append(allDiagnostics, collisionDiagnostics...)}
}

// userAgentsSkillsDirPath resolves the user-level .agents/skills directory
// (~/.agents/skills), the same way the trust check resolves it so the project
// walk can filter it out.
func userAgentsSkillsDirPath() string {
	home := homeDir()
	if override := os.Getenv("HOME"); override != "" {
		home = override
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(CanonicalizePath(ResolvePath(home, "", PathInputOptions{})), ".agents", "skills")
}

// findGitRepoRoot returns the directory containing .git at or above startDir,
// or "" when there is none (upstream findGitRepoRoot).
func findGitRepoRoot(startDir string) string {
	dir := startDir
	for {
		if PathExists(filepath.Join(dir, ".git")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// collectAncestorAgentsSkillDirs lists .agents/skills from startDir up to and
// including the git repository root, or the filesystem root when there is none
// (upstream collectAncestorAgentsSkillDirs).
func collectAncestorAgentsSkillDirs(startDir string) []string {
	resolved := CanonicalizePath(ResolvePath(startDir, "", PathInputOptions{}))
	gitRoot := findGitRepoRoot(resolved)
	var dirs []string
	dir := resolved
	for {
		dirs = append(dirs, filepath.Join(dir, ".agents", "skills"))
		if gitRoot != "" && dir == gitRoot {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dirs
}

// projectAgentsSkillDirs is collectAncestorAgentsSkillDirs gated on project
// trust, with the user directory removed so it is not loaded twice (upstream
// filters the resolved user dir out of the project set).
func projectAgentsSkillDirs(cwd string, trusted bool, userAgentsSkillsDir string) []string {
	if !trusted {
		return nil
	}
	normalizedUser := filepath.Clean(userAgentsSkillsDir)
	dirs := collectAncestorAgentsSkillDirs(cwd)
	filtered := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if filepath.Clean(dir) == normalizedUser {
			continue
		}
		filtered = append(filtered, dir)
	}
	return filtered
}

// escapeXML escapes XML entities (port of escapeXml).
func escapeXML(str string) string {
	str = strings.ReplaceAll(str, "&", "&amp;")
	str = strings.ReplaceAll(str, "<", "&lt;")
	str = strings.ReplaceAll(str, ">", "&gt;")
	str = strings.ReplaceAll(str, `"`, "&quot;")
	return strings.ReplaceAll(str, "'", "&apos;")
}

// FormatSkillsForPrompt renders the available_skills block (Agent Skills
// XML format). Skills with disableModelInvocation are excluded.
func FormatSkillsForPrompt(skills []Skill, fileReadTool string) string {
	var visible []Skill
	for _, skill := range skills {
		if !skill.DisableModelInvocation {
			visible = append(visible, skill)
		}
	}
	if len(visible) == 0 {
		return ""
	}
	lines := []string{
		"\n\nThe following skills provide specialized instructions for specific tasks.",
		"Use the read tool to load a skill's file when the task matches its description.",
		"When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.",
		"",
		"<available_skills>",
	}
	if fileReadTool == "bash" {
		lines[1] = "Use bash to load a skill's file when the task matches its description."
	}
	for _, skill := range visible {
		lines = append(lines,
			"  <skill>",
			"    <name>"+escapeXML(skill.Name)+"</name>",
			"    <description>"+escapeXML(skill.Description)+"</description>",
			"    <location>"+escapeXML(skill.FilePath)+"</location>",
			"  </skill>")
	}
	lines = append(lines, "</available_skills>")
	return strings.Join(lines, "\n")
}
