package validation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/naoufalelbani/init/internal/analyzer"
)

type ToolCheck struct {
	Command      string
	Alternatives []string
	Sources      []string
	Path         string
	Err          error
}

// CheckProject verifies the executable prerequisites named by detected build
// and test hints. It does not execute project commands.
func CheckProject(root string, project analyzer.Project, goos string, lookPath func(string) (string, error)) []ToolCheck {
	requirements := map[string]*ToolCheck{}
	if project.Git {
		requirements["git"] = &ToolCheck{Command: "git", Sources: []string{"Git repository operations"}}
	}
	add := func(command string, alternatives []string, source string) {
		key := strings.ToLower(command)
		check := requirements[key]
		if check == nil {
			check = &ToolCheck{Command: command, Alternatives: alternatives}
			requirements[key] = check
		}
		check.Sources = append(check.Sources, source)
	}
	for _, language := range project.Languages {
		switch language {
		case "Go":
			add("go", nil, "Go project")
		case "Rust":
			add("cargo", nil, "Rust project")
		case "Dart":
			add("dart", nil, "Dart project")
		case "Python":
			if goos == "windows" {
				add("python", []string{"python3"}, "Python project")
			} else {
				add("python3", []string{"python"}, "Python project")
			}
		case "JavaScript", "TypeScript":
			add("node", nil, "JavaScript/TypeScript project")
		case "Java", "Kotlin / Java":
			add("java", nil, "Java project")
		case "C# / .NET":
			add("dotnet", nil, ".NET project")
		case "C / C++":
			add("cmake", nil, "CMake project")
		}
	}
	for _, framework := range project.Frameworks {
		if framework == "Flutter" {
			add("flutter", nil, "Flutter project")
		}
	}
	for _, manager := range project.DependencyManagers {
		lower := strings.ToLower(manager)
		for _, tool := range []string{"npm", "pnpm", "yarn", "bun"} {
			if strings.HasPrefix(lower, tool) {
				add(tool, nil, "project dependency manager")
			}
		}
	}
	for _, hint := range append(append([]string{}, project.Build...), project.Tests...) {
		fields := strings.Fields(hint)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "unknown") || strings.HasPrefix(hint, "inspect ") {
			continue
		}
		command := fields[0]
		key := strings.ToLower(command)
		check := requirements[key]
		if check == nil {
			check = &ToolCheck{Command: command}
			requirements[key] = check
		}
		check.Sources = append(check.Sources, hint)
	}

	checks := make([]ToolCheck, 0, len(requirements))
	commands := make([]string, 0, len(requirements))
	for command := range requirements {
		commands = append(commands, command)
	}
	sort.Strings(commands)
	for _, key := range commands {
		requirement := requirements[key]
		command := requirement.Command
		if isLocalCommand(command, goos) {
			localPath := command
			if strings.HasPrefix(localPath, "./") {
				localPath = filepath.FromSlash(strings.TrimPrefix(localPath, "./"))
			} else if strings.HasPrefix(localPath, `..\`) {
				localPath = filepath.Clean(localPath)
			}
			fullPath := filepath.Join(root, localPath)
			info, err := os.Stat(fullPath)
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("%s is not a regular file", fullPath)
			}
			if err == nil && goos != "windows" && info.Mode().Perm()&0111 == 0 {
				err = fmt.Errorf("%s is not executable", fullPath)
			}
			requirement.Path, requirement.Err = fullPath, err
		} else {
			candidates := append([]string{command}, requirement.Alternatives...)
			for _, candidate := range candidates {
				path, err := lookPath(candidate)
				if err == nil {
					requirement.Path = path
					requirement.Err = nil
					break
				}
				requirement.Err = err
			}
			if requirement.Path == "" && len(candidates) > 1 {
				requirement.Err = fmt.Errorf("none of %s found on PATH", strings.Join(candidates, " or "))
			}
		}
		sort.Strings(requirement.Sources)
		checks = append(checks, *requirement)
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].Command < checks[j].Command })
	return checks
}

func isLocalCommand(command, goos string) bool {
	return strings.HasPrefix(command, "./") || strings.HasPrefix(command, `..\`) || strings.ContainsAny(command, `/\\`) || (goos == "windows" && (strings.HasSuffix(strings.ToLower(command), ".cmd") || strings.HasSuffix(strings.ToLower(command), ".bat")))
}

// CheckProjectWithDefaults is the production entry point for project checks.
func CheckProjectWithDefaults(root string, project analyzer.Project) []ToolCheck {
	return CheckProject(root, project, runtime.GOOS, exec.LookPath)
}
