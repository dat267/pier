package coding

import (
	"os"
	"path/filepath"
	"strings"
)

// Port of the context-file discovery in core/resource-loader.ts and the git
// path discovery it depends on from core/footer-data-provider.ts.

// GitPaths are the git metadata paths for a repository or linked worktree.
type GitPaths struct {
	RepoDir      string
	CommonGitDir string
	HeadPath     string
}

// FindGitPaths walks up from cwd looking for git metadata, handling both
// regular repositories (.git is a directory) and linked worktrees (.git is a
// file with a gitdir: pointer).
func FindGitPaths(cwd string) *GitPaths {
	dir := cwd
	for {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			if info.Mode().IsRegular() {
				content, err := os.ReadFile(gitPath)
				if err != nil {
					return nil
				}
				trimmed := strings.TrimSpace(DecodeUTF8WithReplacement(content))
				if after, ok := strings.CutPrefix(trimmed, "gitdir: "); ok {
					gitDir := resolveFrom(dir, strings.TrimSpace(after))
					headPath := filepath.Join(gitDir, "HEAD")
					if !fileExists(headPath) {
						return nil
					}
					commonGitDir := gitDir
					commonDirPath := filepath.Join(gitDir, "commondir")
					if _, err := os.Stat(commonDirPath); err == nil {
						content, err := os.ReadFile(commonDirPath)
						if err != nil {
							return nil
						}
						commonGitDir = resolveFrom(gitDir, strings.TrimSpace(DecodeUTF8WithReplacement(content)))
					}
					return &GitPaths{RepoDir: dir, CommonGitDir: commonGitDir, HeadPath: headPath}
				}
			} else if info.IsDir() {
				headPath := filepath.Join(gitPath, "HEAD")
				if !fileExists(headPath) {
					return nil
				}
				return &GitPaths{RepoDir: dir, CommonGitDir: gitPath, HeadPath: headPath}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// resolveFrom resolves a possibly relative path against a base directory.
func resolveFrom(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

// contextFileCandidates are the context filenames in preference order.
var contextFileCandidates = []string{"AGENTS.override.md", "AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"}

// LoadContextFileFromDir returns the preferred context file in one directory,
// or nil (upstream loadContextFileFromDir). Directories that merely share a
// candidate name are skipped.
func LoadContextFileFromDir(dir string) *ContextFile {
	for _, filename := range contextFileCandidates {
		filePath := filepath.Join(dir, filename)
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		return &ContextFile{Path: filePath, Content: StripBom(DecodeUTF8WithReplacement(content))}
	}
	return nil
}

// FindShadowedContextFile returns the main repository's context file that a
// nested linked worktree's own copy shadows, or nil.
//
// Both files occupy the same logical repository scope, so loading both would
// apply that context twice. The result is canonicalized because git writes the
// worktree's gitdir target in realpath form while cwd may still be a symlink
// (macOS /tmp -> /private/tmp).
func FindShadowedContextFile(cwd string) *string {
	gitPaths := FindGitPaths(cwd)
	if gitPaths == nil {
		return nil
	}
	commonGitDir := CanonicalizePath(gitPaths.CommonGitDir)
	worktreeRoot := CanonicalizePath(gitPaths.RepoDir)
	mainRepoRoot := filepath.Dir(commonGitDir)
	// False for an ordinary repository, where the two are the same directory,
	// and for a sibling worktree, whose main repo is not an ancestor.
	if !strings.HasPrefix(worktreeRoot, mainRepoRoot+string(filepath.Separator)) {
		return nil
	}
	// dirname of the common git dir is the main worktree root only when that
	// directory is itself checked out from the same repository. In a bare
	// layout it merely holds the bare git dir, and a submodule's gitdir has no
	// commondir, so it lands under .git/modules.
	if CanonicalizePath(filepath.Join(mainRepoRoot, ".git")) != commonGitDir {
		return nil
	}
	worktreeContextFile := LoadContextFileFromDir(worktreeRoot)
	if worktreeContextFile == nil {
		return nil
	}
	shadowed := filepath.Join(mainRepoRoot, filepath.Base(worktreeContextFile.Path))
	return &shadowed
}

// LoadProjectContextFiles collects the global agent context file followed by
// the ancestor context files from the outermost directory to cwd (upstream
// loadProjectContextFiles).
func LoadProjectContextFiles(cwd, agentDir string) []ContextFile {
	resolvedCwd := ResolvePath(cwd, ".", PathInputOptions{})
	resolvedAgentDir := ResolvePath(agentDir, ".", PathInputOptions{})

	var contextFiles []ContextFile
	seenPaths := map[string]bool{}

	if globalContext := LoadContextFileFromDir(resolvedAgentDir); globalContext != nil {
		contextFiles = append(contextFiles, *globalContext)
		seenPaths[globalContext.Path] = true
	}

	var ancestorContextFiles []ContextFile
	shadowedContextFile := FindShadowedContextFile(resolvedCwd)
	currentDir := resolvedCwd

	for {
		contextFile := LoadContextFileFromDir(currentDir)
		// Compare the directory's real path, not the file's: a worktree
		// AGENTS.md that symlinks to the main repo's file resolves to the
		// shadowed path itself.
		isShadowed := shadowedContextFile != nil && contextFile != nil &&
			filepath.Join(CanonicalizePath(currentDir), filepath.Base(contextFile.Path)) == *shadowedContextFile
		if contextFile != nil && !isShadowed && !seenPaths[contextFile.Path] {
			ancestorContextFiles = append([]ContextFile{*contextFile}, ancestorContextFiles...)
			seenPaths[contextFile.Path] = true
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}
		currentDir = parentDir
	}

	return append(contextFiles, ancestorContextFiles...)
}
