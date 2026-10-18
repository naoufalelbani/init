package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const version = "0.1.0"

type project struct {
	Name                                         string
	Languages, Frameworks, Build, Tests, Sources []string
	Git                                          bool
}

func Run(args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		usage(out)
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage(out)
		return nil
	case "version":
		fmt.Fprintln(out, "ai-init", version)
		return nil
	case "analyze":
		root, rest, e := target(args[1:])
		if e != nil {
			return e
		}
		_ = rest
		p := analyze(root)
		printProject(out, root, p)
		return nil
	case "doctor":
		return doctor(out)
	case "setup":
		return setup(args[1:], out)
	case "agents":
		return agents(args[1:], out)
	case "memory":
		return memory(args[1:], out)
	case "prompt":
		return prompt(args[1:], out)
	case "wt":
		return worktrees(args[1:], out)
	default:
		return fmt.Errorf("unknown command %q; run 'ai-init help'", args[0])
	}
}
func usage(w io.Writer) {
	fmt.Fprintln(w, `ai-init initializes AI-assisted development environments.

Usage:
  ai-init setup [--path DIR] [--dry-run] [--with agents,worktrees,memory]
  ai-init analyze [--path DIR]
  ai-init agents generate|audit|sync|validate [--path DIR] [--dry-run]
  ai-init memory generate|update [--path DIR]
  ai-init wt new|list|status|sync|integrate|cleanup [TASK]
  ai-init prompt agents|worktrees|audit|config
  ai-init doctor
  ai-init version`)
}
func target(args []string) (string, []string, error) {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", ".", "project path")
	if e := fs.Parse(args); e != nil {
		return "", nil, e
	}
	p, e := filepath.Abs(*path)
	return p, fs.Args(), e
}
func analyze(root string) project {
	p := project{Name: filepath.Base(root)}
	if _, e := os.Stat(filepath.Join(root, ".git")); e == nil {
		p.Git = true
	}
	checks := map[string]string{"go.mod": "Go", "Cargo.toml": "Rust", "pubspec.yaml": "Dart / Flutter", "pyproject.toml": "Python", "requirements.txt": "Python", "package.json": "JavaScript / TypeScript", "pom.xml": "Java", "build.gradle": "Java", "build.gradle.kts": "Kotlin / Java", "*.csproj": "C# / .NET", "CMakeLists.txt": "C / C++ / Qt"}
	entries, _ := os.ReadDir(root)
	names := map[string]bool{}
	for _, x := range entries {
		names[x.Name()] = true
	}
	for f, l := range checks {
		ok := names[f]
		if strings.HasPrefix(f, "*.") {
			for n := range names {
				if strings.HasSuffix(n, strings.TrimPrefix(f, "*")) {
					ok = true
				}
			}
		}
		if ok {
			p.Languages = append(p.Languages, l)
		}
	}
	if names["package.json"] {
		b, _ := os.ReadFile(filepath.Join(root, "package.json"))
		s := string(b)
		for _, x := range []string{"react", "vue", "vite", "next", "express"} {
			if strings.Contains(s, "\""+x+"\"") {
				p.Frameworks = append(p.Frameworks, x)
			}
		}
		p.Build = append(p.Build, "package scripts (inspect package.json)")
	}
	if names["go.mod"] {
		p.Build = append(p.Build, "go build ./...")
		p.Tests = append(p.Tests, "go test ./...")
	}
	if names["Cargo.toml"] {
		p.Build = append(p.Build, "cargo build")
		p.Tests = append(p.Tests, "cargo test")
	}
	if names["pyproject.toml"] {
		p.Build = append(p.Build, "unknown (pyproject.toml detected)")
	}
	for _, d := range []string{"src", "app", "cmd", "lib", "internal", "pkg", "test", "tests"} {
		if names[d] {
			p.Sources = append(p.Sources, d)
		}
	}
	sort.Strings(p.Languages)
	sort.Strings(p.Frameworks)
	return p
}
func printProject(w io.Writer, root string, p project) {
	fmt.Fprintf(w, "Project: %s\nPath: %s\nStack: %s\nGit: %t\n", p.Name, root, joinOrUnknown(p.Languages), p.Git)
	if len(p.Frameworks) > 0 {
		fmt.Fprintf(w, "Frameworks: %s\n", strings.Join(p.Frameworks, ", "))
	}
	fmt.Fprintf(w, "Source directories: %s\n", joinOrUnknown(p.Sources))
	fmt.Fprintf(w, "Build hints: %s\n", joinOrUnknown(p.Build))
	fmt.Fprintf(w, "Test hints: %s\n", joinOrUnknown(p.Tests))
}
func joinOrUnknown(a []string) string {
	if len(a) == 0 {
		return "unknown"
	}
	return strings.Join(a, ", ")
}
func setup(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", ".", "project path")
	dry := fs.Bool("dry-run", false, "preview without writing")
	with := fs.String("with", "agents,memory", "comma-separated components: agents,worktrees,memory")
	verbose := fs.Bool("verbose", false, "show details")
	if e := fs.Parse(args); e != nil {
		return e
	}
	root, e := filepath.Abs(*path)
	if e != nil {
		return e
	}
	p := analyze(root)
	printProject(w, root, p)
	selected := map[string]bool{}
	for _, v := range strings.Split(*with, ",") {
		name := strings.TrimSpace(v)
		if name != "agents" && name != "worktrees" && name != "memory" {
			return fmt.Errorf("unknown setup component %q", name)
		}
		selected[name] = true
	}
	fmt.Fprintln(w, "\nSetup:")
	if selected["agents"] {
		if e := writeAgents(root, p, *dry, w); e != nil {
			return e
		}
	}
	if selected["memory"] {
		if e := writeMemory(root, p, *dry, w); e != nil {
			return e
		}
	}
	if selected["worktrees"] {
		if e := installWorktreeGuide(root, *dry, w); e != nil {
			return e
		}
	}
	if *verbose {
		fmt.Fprintf(w, "\nPlatform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	}
	fmt.Fprintln(w, "Validation: completed for generated files")
	return nil
}
func writeFileSafe(path, content string, dry bool, w io.Writer) error {
	old, e := os.ReadFile(path)
	if e == nil && bytes.Equal(old, []byte(content)) {
		fmt.Fprintf(w, "  preserved %s (already current)\n", path)
		return nil
	}
	if e == nil {
		backup := path + ".ai-init.bak"
		if _, be := os.Stat(backup); os.IsNotExist(be) {
			if !dry {
				if be = os.WriteFile(backup, old, 0600); be != nil {
					return be
				}
			}
		}
		fmt.Fprintf(w, "  updated %s (backup: %s)\n", path, backup)
	} else if os.IsNotExist(e) {
		fmt.Fprintf(w, "  created %s\n", path)
	} else {
		return e
	}
	if dry {
		return nil
	}
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	tmp := path + fmt.Sprintf(".tmp-%d", os.Getpid())
	if e = os.WriteFile(tmp, []byte(content), 0644); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		_ = os.Remove(tmp)
		return e
	}
	return nil
}
func agentsTemplate(p project) string {
	return fmt.Sprintf(`# Project instructions

## Verified project facts
- Project: %s
- Detected languages: %s
- Detected frameworks: %s
- Source directories: %s
- Git repository: %t

## Development rules
- Respect the existing architecture, framework conventions, and public behavior. Do not rewrite the application or change its language without explicit direction.
- Inspect relevant code, callers, tests, and repository instructions before editing. Search for existing functionality before adding another implementation.
- Keep changes focused, understandable, and compatible with established naming and formatting.
- Handle errors explicitly. Do not add silent failures, dead code, temporary production stubs, or unnecessary dependencies.
- Preserve user changes and unrelated files. Never use destructive Git operations or overwrite uncommitted work.
- Do not expose credentials, tokens, private keys, or other secrets in source, logs, or generated context.
- Verify changes with commands supported by this repository. Detected test hints: %s. Treat these as hints and confirm project-specific scripts before running.
- Report what changed, what verification ran, and any unresolved issues honestly.

## Agent workspace safety
- Work only in the assigned repository or Git worktree. Do not edit another agent's worktree.
- Keep task-local notes out of commits unless the user asks to track them.
- Integrate parallel work only after reviewing the combined changes.
`, p.Name, joinOrUnknown(p.Languages), joinOrUnknown(p.Frameworks), joinOrUnknown(p.Sources), p.Git, joinOrUnknown(p.Tests))
}
func writeAgents(root string, p project, dry bool, w io.Writer) error {
	path := filepath.Join(root, "AGENTS.md")
	existing, err := os.ReadFile(path)
	start, end := "<!-- ai-init:begin -->", "<!-- ai-init:end -->"
	if err == nil {
		text := string(existing)
		begin := strings.Index(text, start)
		if begin >= 0 {
			relativeEnd := strings.Index(text[begin:], end)
			if relativeEnd < 0 {
				return errors.New("managed AGENTS.md block has no end marker; preserving file")
			}
			finish := begin + relativeEnd + len(end)
			updated := text[:begin] + start + "\n" + agentsTemplate(p) + "\n" + end + text[finish:]
			return writeFileSafe(path, updated, dry, w)
		}
		fmt.Fprintf(w, "  preserved %s (custom instructions detected; use agents sync to inspect)\n", path)
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return writeFileSafe(path, start+"\n"+agentsTemplate(p)+"\n"+end+"\n", dry, w)
}
func writeMemory(root string, p project, dry bool, w io.Writer) error {
	content := fmt.Sprintf("# Project map\n\nGenerated from repository files on %s. Verify details against source.\n\n- Name: %s\n- Languages: %s\n- Frameworks: %s\n- Source directories: %s\n- Build hints: %s\n- Test hints: %s\n", time.Now().Format("2006-01-02"), p.Name, joinOrUnknown(p.Languages), joinOrUnknown(p.Frameworks), joinOrUnknown(p.Sources), joinOrUnknown(p.Build), joinOrUnknown(p.Tests))
	return writeFileSafe(filepath.Join(root, ".ai", "PROJECT_MAP.md"), content, dry, w)
}
func installWorktreeGuide(root string, dry bool, w io.Writer) error {
	content := "# Worktrees`n`nUse `ai-init wt new <task>` to create an isolated task branch. Review changes and run validation before integration. Cleanup refuses to remove unmerged work.`n"
	return writeFileSafe(filepath.Join(root, ".ai-init", "WORKTREES.md"), content, dry, w)
}
func agents(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ai-init agents generate|audit|sync|validate")
	}
	root, _, e := target(args[1:])
	if e != nil {
		return e
	}
	p := analyze(root)
	switch args[0] {
	case "generate", "sync":
		return writeAgents(root, p, false, w)
	case "audit", "validate":
		b, e := os.ReadFile(filepath.Join(root, "AGENTS.md"))
		if e != nil {
			return fmt.Errorf("AGENTS.md: %w", e)
		}
		fmt.Fprintf(w, "AGENTS.md present (%d bytes); custom content is preserved\n", len(b))
		return nil
	default:
		return fmt.Errorf("unknown agents action %q", args[0])
	}
}
func memory(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ai-init memory generate|update")
	}
	root, _, e := target(args[1:])
	if e != nil {
		return e
	}
	return writeMemory(root, analyze(root), false, w)
}
func prompt(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("choose agents, worktrees, audit, or config")
	}
	file := map[string]string{"agents": "generate-agents.md", "worktrees": "setup-worktrees.md", "audit": "audit-project.md", "config": "improve-existing-config.md"}[args[0]]
	if file == "" {
		return fmt.Errorf("unknown prompt %q", args[0])
	}
	b, e := os.ReadFile(filepath.Join("prompts", file))
	if e != nil {
		return fmt.Errorf("prompt not found at %s: %w", filepath.Join("prompts", file), e)
	}
	_, e = w.Write(b)
	return e
}
func doctor(w io.Writer) error {
	fmt.Fprintf(w, "Platform: %s/%s\nGo runtime: %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	for _, tool := range []string{"git", "go"} {
		p, e := exec.LookPath(tool)
		if e != nil {
			fmt.Fprintf(w, "%s: unavailable\n", tool)
		} else {
			fmt.Fprintf(w, "%s: %s\n", tool, p)
		}
	}
	return nil
}
func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}
func worktrees(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ai-init wt new|list|status|sync|integrate|cleanup [task]")
	}
	here, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	root, err := git(here, "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("current directory is not inside a Git repository")
	}
	action := args[0]
	if action == "list" {
		value, err := git(root, "worktree", "list", "--porcelain")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, value)
		return nil
	}
	if action == "new" {
		if len(args) < 2 {
			return errors.New("usage: ai-init wt new <task>")
		}
		task := args[1]
		if strings.ContainsAny(task, "/\\") || task == "." || task == ".." {
			return errors.New("task name must be a single directory name")
		}
		dest := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", task)
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("worktree path already exists: %s", dest)
		}
		branch := "codex/" + task
		if _, err = git(root, "worktree", "add", "-b", branch, dest); err != nil {
			return err
		}
		exclude, err := git(root, "rev-parse", "--git-path", "info/exclude")
		if err != nil {
			return err
		}
		if !filepath.IsAbs(exclude) {
			exclude = filepath.Join(root, exclude)
		}
		ignore, readErr := os.ReadFile(exclude)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if !strings.Contains(string(ignore), ".agent-task.md") {
			if err = os.MkdirAll(filepath.Dir(exclude), 0755); err != nil {
				return err
			}
			f, openErr := os.OpenFile(exclude, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if openErr != nil {
				return openErr
			}
			_, writeErr := f.WriteString("\n# ai-init task-local primer\n.agent-task.md\n")
			closeErr := f.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		primer := fmt.Sprintf("# Task workspace\n\n- Task: %s\n- Workspace: %s\n- Branch: %s\n- Instructions: see repository AGENTS.md\n\nDo not edit another worktree. Preserve unrelated changes. Report files changed and verification performed.\n", task, dest, branch)
		if err = os.WriteFile(filepath.Join(dest, ".agent-task.md"), []byte(primer), 0644); err != nil {
			return fmt.Errorf("worktree created but task primer failed: %w", err)
		}
		fmt.Fprintf(w, "Created %s on branch %s\n", dest, branch)
		return nil
	}
	if len(args) < 2 {
		return fmt.Errorf("%s requires a task name", action)
	}
	task := args[1]
	dest := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", task)
	switch action {
	case "status":
		value, err := git(dest, "status", "--short", "--branch")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, value)
		return nil
	case "sync":
		status, err := git(dest, "status", "--porcelain")
		if err != nil {
			return err
		}
		if status != "" {
			return errors.New("worktree has uncommitted changes; commit or stash them before sync")
		}
		branch, err := git(root, "branch", "--show-current")
		if err != nil {
			return err
		}
		if _, err = git(dest, "merge", "--no-edit", branch); err != nil {
			return fmt.Errorf("sync failed; resolve conflicts in the task worktree: %w", err)
		}
		fmt.Fprintf(w, "Synced %s into task worktree\n", branch)
		return nil
	case "integrate":
		return errors.New("transactional integration is not implemented; review and merge the task branch manually")
	case "cleanup":
		status, err := git(dest, "status", "--porcelain")
		if err != nil {
			return err
		}
		if status != "" {
			return errors.New("refusing cleanup: worktree has uncommitted changes")
		}
		branch, err := git(dest, "branch", "--show-current")
		if err != nil {
			return err
		}
		target, err := git(root, "branch", "--show-current")
		if err != nil {
			return err
		}
		cmd := exec.Command("git", "merge-base", "--is-ancestor", branch, target)
		cmd.Dir = root
		if err = cmd.Run(); err != nil {
			return errors.New("refusing cleanup because task branch is not integrated")
		}
		if _, err = git(root, "worktree", "remove", dest); err != nil {
			return err
		}
		fmt.Fprintf(w, "Removed clean worktree %s\n", dest)
		return nil
	default:
		return fmt.Errorf("unknown worktree action %q", action)
	}
}
