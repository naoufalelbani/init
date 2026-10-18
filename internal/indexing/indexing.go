// Package indexing builds a small, Git-aware inventory of project source paths.
// It deliberately records paths only and never reads source file contents.
package indexing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxFiles = 60

type Inventory struct {
	Files     []string
	Total     int
	Truncated bool
	Source    string
}

// Discover prefers Git's index and non-ignored untracked files when root is a
// repository. If Git is unavailable or cannot inspect the repository, it uses
// a bounded-purpose filesystem scan that skips common generated directories.
func Discover(root string, gitRepository bool) (Inventory, error) {
	source := "filesystem scan"
	var paths []string
	if gitRepository {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
		output, err := command.Output()
		if err == nil {
			paths = strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
			if len(paths) == 1 && paths[0] == "" {
				paths = nil
			}
			source = "Git index and non-ignored files"
		} else {
			source = "filesystem scan (Git inventory unavailable)"
		}
	}
	if source == "filesystem scan" || source == "filesystem scan (Git inventory unavailable)" {
		var err error
		paths, err = walkSourcePaths(root)
		if err != nil {
			return Inventory{}, err
		}
	}
	files := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if path == "" || !isSourcePath(path) || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	sort.Strings(files)
	inventory := Inventory{Total: len(files), Source: source}
	if len(files) > maxFiles {
		inventory.Truncated = true
		files = files[:maxFiles]
	}
	inventory.Files = files
	return inventory, nil
}

func walkSourcePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if excludedDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func isSourcePath(path string) bool {
	parts := strings.Split(strings.ToLower(path), "/")
	for _, part := range parts[:len(parts)-1] {
		if excludedDirectory(part) {
			return false
		}
	}
	name := parts[len(parts)-1]
	if strings.Contains(name, "secret") || strings.Contains(name, "credential") || strings.Contains(name, "private_key") ||
		strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".p12") || strings.HasSuffix(name, ".pfx") {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".rs", ".dart", ".py", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".vue", ".svelte",
		".java", ".kt", ".kts", ".cs", ".cpp", ".cxx", ".cc", ".c", ".h", ".hpp", ".hxx", ".m", ".mm", ".swift",
		".php", ".rb", ".ex", ".exs", ".scala", ".sc", ".sql", ".sh", ".ps1", ".proto", ".graphql", ".gql":
		return true
	default:
		return false
	}
}

func excludedDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".ai", ".ai-init", ".graphify", ".serena", "node_modules", "vendor", "target", "dist", "build", "coverage", ".venv", "venv", "pods", "deriveddata", ".next", ".nuxt":
		return true
	default:
		return false
	}
}
