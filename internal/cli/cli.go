package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	agentcfg "github.com/naoufalelbani/init/internal/agents"
	"github.com/naoufalelbani/init/internal/analyzer"
	envprop "github.com/naoufalelbani/init/internal/environment"
	hookguard "github.com/naoufalelbani/init/internal/hooks"
	"github.com/naoufalelbani/init/internal/indexing"
	portalloc "github.com/naoufalelbani/init/internal/ports"
	"github.com/naoufalelbani/init/internal/postgres"
	"github.com/naoufalelbani/init/internal/validation"
)

const version = "0.1.0"
const postgresOperationTimeout = 2 * time.Minute

// runPostgresClient is the process seam for database lifecycle tests. Production
// uses argument-based execution and never logs client output or credentials.
var runPostgresClient postgres.Runner = postgres.ExecRunner
var reuseInstalledIndexers = indexing.UseInstalled

type project = analyzer.Project

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
		p, e := analyzer.Analyze(root)
		if e != nil {
			return e
		}
		printProject(out, root, p)
		return nil
	case "doctor":
		return doctor(args[1:], out)
	case "setup":
		return setup(args[1:], out)
	case "agents":
		return agents(args[1:], out)
	case "memory":
		return memory(args[1:], out)
	case "ports":
		return ports(args[1:], out)
	case "hooks":
		return hooks(args[1:], out)
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
  ai-init setup [--path DIR] [--dry-run|--diff] [--with agents,worktrees,memory,gitattributes,hooks] [--use-indexers]
  ai-init analyze [--path DIR]
  ai-init agents generate|audit|sync|validate|configure|list [--path DIR] [--adapter NAME] [--diff]
  ai-init memory generate|update [--path DIR] [--use-indexers]
  ai-init ports allocate|list|release [TASK] [SERVICE] [--path DIR]
  ai-init hooks check-staged [--path DIR]
  ai-init wt new <TASK> [--ide ADAPTER] [--port SERVICE] [--env NAME] [--isolate-postgres-env NAME --postgres-admin-env NAME] [--dry-run]
  ai-init wt open <TASK> --ide ADAPTER
  ai-init wt list|status|sync|integrate|cleanup [TASK] [--drop-database --postgres-admin-env NAME]
  ai-init prompt agents|worktrees|audit|config [--output FILE] [--dry-run]
  ai-init doctor [--path DIR]
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
	if e != nil {
		return "", nil, e
	}
	if !pathSpecified(args) {
		p = discoverRoot(p)
	}
	return p, fs.Args(), nil
}

func pathSpecified(args []string) bool {
	for _, arg := range args {
		if arg == "--path" || strings.HasPrefix(arg, "--path=") {
			return true
		}
	}
	return false
}

func discoverRoot(start string) string {
	if top, err := git(start, "rev-parse", "--show-toplevel"); err == nil {
		return top
	}
	markers := []string{"go.mod", "Cargo.toml", "pubspec.yaml", "package.json", "pyproject.toml", "requirements.txt", "pom.xml", "build.gradle", "build.gradle.kts", "CMakeLists.txt"}
	current := start
	for {
		entries, err := os.ReadDir(current)
		if err == nil {
			names := make(map[string]bool, len(entries))
			for _, entry := range entries {
				names[entry.Name()] = true
			}
			for _, marker := range markers {
				if names[marker] {
					return current
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
}
func printProject(w io.Writer, root string, p project) {
	fmt.Fprintf(w, "Project: %s\nPath: %s\nStack: %s\nGit: %t\n", p.Name, root, joinOrUnknown(p.Languages), p.Git)
	if len(p.Frameworks) > 0 {
		fmt.Fprintf(w, "Frameworks: %s\n", strings.Join(p.Frameworks, ", "))
	}
	fmt.Fprintf(w, "Source directories: %s\n", joinOrUnknown(p.Sources))
	fmt.Fprintf(w, "Entry points: %s\n", joinOrUnknown(p.EntryPoints))
	fmt.Fprintf(w, "Dependency managers: %s\n", joinOrUnknown(p.DependencyManagers))
	fmt.Fprintf(w, "Project configuration: %s\n", joinOrUnknown(p.Configurations))
	fmt.Fprintf(w, "Agent instructions: %s\n", joinOrUnknown(p.AgentInstructions))
	fmt.Fprintf(w, "Build hints: %s\n", joinOrUnknown(p.Build))
	fmt.Fprintf(w, "Test hints: %s\n", joinOrUnknown(p.Tests))
}
func joinOrUnknown(a []string) string {
	if len(a) == 0 {
		return "unknown"
	}
	return strings.Join(a, ", ")
}

func markdownFacts(values []string) string {
	if len(values) == 0 {
		return "unknown"
	}
	formatted := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, value)
		maxBackticks, current := 0, 0
		for _, r := range value {
			if r == '`' {
				current++
				if current > maxBackticks {
					maxBackticks = current
				}
			} else {
				current = 0
			}
		}
		delimiter := strings.Repeat("`", maxBackticks+1)
		formatted = append(formatted, delimiter+value+delimiter)
	}
	return strings.Join(formatted, ", ")
}
func setup(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", ".", "project path")
	dry := fs.Bool("dry-run", false, "preview without writing")
	diff := fs.Bool("diff", false, "show AGENTS.md changes without writing")
	with := fs.String("with", "agents,memory", "comma-separated components: agents,worktrees,memory,gitattributes,hooks")
	adapters := fs.String("adapters", "", "comma-separated agent adapters; empty detects existing configuration")
	verbose := fs.Bool("verbose", false, "show details")
	useIndexers := fs.Bool("use-indexers", false, "reuse existing Serena or Graphify indexes while generating memory (explicit opt-in)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if *diff {
		*dry = true
	}
	root, e := filepath.Abs(*path)
	if e != nil {
		return e
	}
	if !pathSpecified(args) {
		root = discoverRoot(root)
	}
	selected := map[string]bool{}
	for _, v := range strings.Split(*with, ",") {
		name := strings.TrimSpace(v)
		if name != "agents" && name != "worktrees" && name != "memory" && name != "gitattributes" && name != "hooks" {
			return fmt.Errorf("unknown setup component %q", name)
		}
		selected[name] = true
	}
	for _, adapter := range splitList(*adapters) {
		if !slices.Contains(agentcfg.Names(), strings.ToLower(adapter)) {
			return fmt.Errorf("unknown agent adapter %q; run 'ai-init agents list'", adapter)
		}
	}
	if len(splitList(*adapters)) > 0 && !selected["agents"] {
		return errors.New("--adapters requires the agents component in --with")
	}
	if *useIndexers && !selected["memory"] {
		return errors.New("--use-indexers requires the memory component in --with")
	}
	p, e := analyzer.Analyze(root)
	if e != nil {
		return e
	}
	printProject(w, root, p)
	fmt.Fprintln(w, "\nSetup:")
	if selected["agents"] {
		var writeErr error
		if *diff {
			writeErr = writeAgentsDiff(root, p, w)
		} else {
			writeErr = writeAgents(root, p, *dry, w)
		}
		if writeErr != nil {
			return writeErr
		}
		var adapterReport bytes.Buffer
		if e := agentcfg.Configure(root, splitList(*adapters), *dry, &adapterReport); e != nil {
			return e
		}
		if adapterReport.Len() == 0 {
			fmt.Fprintln(w, "  adapters: none detected; use --adapters to configure one explicitly")
		} else {
			_, _ = w.Write(adapterReport.Bytes())
		}
	}
	if selected["memory"] {
		if e := writeMemoryWithIndexers(root, p, *dry, *useIndexers, w); e != nil {
			return e
		}
	}
	if selected["worktrees"] {
		if e := installWorktreeGuide(root, *dry, w); e != nil {
			return e
		}
	}
	if selected["gitattributes"] {
		if e := writeGitAttributes(root, p.Git, *dry, w); e != nil {
			return e
		}
	}
	if selected["hooks"] {
		if e := hookguard.Install(root, *dry, w); e != nil {
			return e
		}
	}
	if *verbose {
		fmt.Fprintf(w, "\nPlatform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	}
	if *dry {
		fmt.Fprintln(w, "Validation: skipped (dry-run; no files were written)")
	} else if err := verifySetup(root, selected); err != nil {
		return fmt.Errorf("setup validation failed: %w", err)
	} else {
		fmt.Fprintln(w, "Validation: passed for selected files")
	}
	return nil
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// runValidationCommand runs only statically generated test commands from analyze.
// Windows validation tools often resolve through .cmd wrappers, so use cmd.exe there.
func runValidationCommand(dir, command string, stdout, stderr io.Writer) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return errors.New("empty validation command")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		args := append([]string{"/d", "/c", "call"}, parts...)
		cmd = exec.Command("cmd.exe", args...)
	} else {
		cmd = exec.Command(parts[0], parts[1:]...)
	}
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func removeMarkedIgnore(path, marker, pattern string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for i := 0; i < len(lines); i++ {
		if strings.TrimSuffix(lines[i], "\r") == marker && i+1 < len(lines) && strings.TrimSuffix(lines[i+1], "\r") == pattern {
			i++
			continue
		}
		kept = append(kept, lines[i])
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")), info.Mode().Perm())
}

func hasIgnorePattern(data []byte, pattern string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(strings.TrimSuffix(line, "\r")) == pattern {
			return true
		}
	}
	return false
}

func appendIgnoreBlock(path, marker, pattern string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(file, "\n%s\n%s\n", marker, pattern)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func acquireWorktreeCreationLock(root string) (func(), error) {
	path, err := git(root, "rev-parse", "--git-path", "ai-init-worktree-create.lock")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = os.Mkdir(path, 0700)
		if err == nil {
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire worktree creation lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for worktree creation lock %s; inspect active processes before removing it", path)
		}
		time.Sleep(25 * time.Millisecond)
	}
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
	template, _ := promptFiles.ReadFile("templates/AGENTS.template.md")
	replacer := strings.NewReplacer(
		"{{PROJECT_NAME}}", markdownFacts([]string{p.Name}),
		"{{LANGUAGES}}", markdownFacts(p.Languages),
		"{{FRAMEWORKS}}", markdownFacts(p.Frameworks),
		"{{DEPENDENCY_MANAGERS}}", markdownFacts(p.DependencyManagers),
		"{{SOURCES}}", markdownFacts(p.Sources),
		"{{ENTRY_POINTS}}", markdownFacts(p.EntryPoints),
		"{{AGENT_INSTRUCTIONS}}", markdownFacts(p.AgentInstructions),
		"{{PROJECT_CONFIGURATIONS}}", markdownFacts(p.Configurations),
		"{{PLATFORM_REQUIREMENTS}}", markdownFacts(p.PlatformRequirements),
		"{{GIT}}", fmt.Sprint(p.Git),
		"{{BUILD_COMMANDS}}", markdownFacts(p.Build),
		"{{TEST_COMMANDS}}", markdownFacts(p.Tests),
	)
	return replacer.Replace(string(template))
}
func plannedAgentsContent(root string, p project) (old, updated []byte, preserved bool, err error) {
	path := filepath.Join(root, "AGENTS.md")
	existing, err := os.ReadFile(path)
	start, end := "<!-- ai-init:begin -->", "<!-- ai-init:end -->"
	if err == nil {
		text := string(existing)
		begin := strings.Index(text, start)
		finishMarker := strings.Index(text, end)
		if begin >= 0 || finishMarker >= 0 {
			if begin < 0 || finishMarker < 0 || finishMarker < begin || strings.Count(text, start) != 1 || strings.Count(text, end) != 1 {
				return nil, nil, false, errors.New("managed AGENTS.md markers are incomplete or out of order; preserving file")
			}
			relativeEnd := strings.Index(text[begin:], end)
			if relativeEnd < 0 {
				return nil, nil, false, errors.New("managed AGENTS.md block has no end marker; preserving file")
			}
			finish := begin + relativeEnd + len(end)
			updated := text[:begin] + start + "\n" + agentsTemplate(p) + "\n" + end + text[finish:]
			return existing, []byte(updated), false, nil
		}
		return existing, existing, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, nil, false, err
	}
	return nil, []byte(start + "\n" + agentsTemplate(p) + "\n" + end + "\n"), false, nil
}

func writeAgents(root string, p project, dry bool, w io.Writer) error {
	path := filepath.Join(root, "AGENTS.md")
	old, updated, preserved, err := plannedAgentsContent(root, p)
	if err != nil {
		return err
	}
	if preserved {
		fmt.Fprintf(w, "  preserved %s (custom instructions detected; use agents sync to inspect)\n", path)
		return nil
	}
	if old != nil && bytes.Equal(old, updated) {
		fmt.Fprintf(w, "  preserved %s (already current)\n", path)
		return nil
	}
	return writeFileSafe(path, string(updated), dry, w)
}

func writeAgentsDiff(root string, p project, w io.Writer) error {
	path := filepath.Join(root, "AGENTS.md")
	old, updated, preserved, err := plannedAgentsContent(root, p)
	if err != nil {
		return err
	}
	if preserved {
		fmt.Fprintf(w, "No AGENTS.md changes proposed; custom instructions at %s are preserved.\n", path)
		return nil
	}
	if bytes.Equal(old, updated) {
		fmt.Fprintf(w, "No AGENTS.md changes; %s is already current.\n", path)
		return nil
	}
	writeUnifiedDiff(w, "AGENTS.md", old, updated)
	return nil
}

func diffLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.SplitAfter(string(data), "\n")
}

func writeUnifiedDiff(w io.Writer, name string, oldData, newData []byte) {
	oldLines, newLines := diffLines(oldData), diffLines(newData)
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix && oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	oldEnd, newEnd := len(oldLines)-suffix, len(newLines)-suffix
	contextStart := prefix - 3
	if contextStart < 0 {
		contextStart = 0
	}
	oldContextEnd, newContextEnd := oldEnd+3, newEnd+3
	if oldContextEnd > len(oldLines) {
		oldContextEnd = len(oldLines)
	}
	if newContextEnd > len(newLines) {
		newContextEnd = len(newLines)
	}
	oldCount, newCount := oldContextEnd-contextStart, newContextEnd-contextStart
	oldStart, newStart := contextStart+1, contextStart+1
	if oldCount == 0 {
		oldStart = contextStart
	}
	if newCount == 0 {
		newStart = contextStart
	}
	oldName, newName := "a/"+name, "b/"+name
	if oldData == nil {
		oldName = "/dev/null"
	}
	if newData == nil {
		newName = "/dev/null"
	}
	fmt.Fprintf(w, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n", oldName, newName, oldStart, oldCount, newStart, newCount)
	for _, line := range oldLines[contextStart:prefix] {
		fmt.Fprintf(w, " %s\n", strings.TrimRight(line, "\r\n"))
	}
	for _, line := range oldLines[prefix:oldEnd] {
		fmt.Fprintf(w, "-%s\n", strings.TrimRight(line, "\r\n"))
	}
	for _, line := range newLines[prefix:newEnd] {
		fmt.Fprintf(w, "+%s\n", strings.TrimRight(line, "\r\n"))
	}
	for _, line := range oldLines[oldEnd:oldContextEnd] {
		fmt.Fprintf(w, " %s\n", strings.TrimRight(line, "\r\n"))
	}
}

func auditAgentsFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("AGENTS.md: %w", err)
	}
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return "", errors.New("AGENTS.md is empty")
	}
	start, end := "<!-- ai-init:begin -->", "<!-- ai-init:end -->"
	begin := strings.Index(text, start)
	finish := strings.Index(text, end)
	if begin < 0 && finish < 0 {
		return fmt.Sprintf("AGENTS.md contains custom instructions (%d bytes); no ai-init managed block", len(data)), nil
	}
	if begin < 0 || finish < 0 || finish < begin || strings.Count(text, start) != 1 || strings.Count(text, end) != 1 {
		return "", errors.New("AGENTS.md managed markers are incomplete or out of order")
	}
	sectionStart := begin + len(start)
	sectionEnd := finish
	if strings.TrimSpace(text[sectionStart:sectionEnd]) == "" {
		return "", errors.New("AGENTS.md managed block is empty")
	}
	return fmt.Sprintf("AGENTS.md managed instructions valid (%d bytes)", len(data)), nil
}

func verifySetup(root string, selected map[string]bool) error {
	var required []string
	if selected["agents"] {
		required = append(required, "AGENTS.md")
	}
	if selected["memory"] {
		for _, name := range []string{"PROJECT_MAP.md", "ARCHITECTURE.md", "CONVENTIONS.md", "CURRENT_STATE.md", "DECISIONS.md"} {
			required = append(required, filepath.Join(".ai", name))
		}
	}
	if selected["worktrees"] {
		required = append(required, filepath.Join(".ai-init", "WORKTREES.md"))
	}
	for _, name := range required {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			return fmt.Errorf("verify %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("verify %s: expected a regular file", name)
		}
	}
	if selected["agents"] {
		if _, err := auditAgentsFile(filepath.Join(root, "AGENTS.md")); err != nil {
			return fmt.Errorf("verify AGENTS.md: %w", err)
		}
	}
	return nil
}

func writeGitAttributes(root string, gitRepo, dry bool, w io.Writer) error {
	const begin = "# ai-init gitattributes:begin"
	const end = "# ai-init gitattributes:end"
	const body = "* text=auto\n*.sh text eol=lf\n*.bash text eol=lf\n*.bat text eol=crlf\n*.cmd text eol=crlf\n*.ps1 text eol=crlf"
	path := filepath.Join(root, ".gitattributes")
	old, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if gitRepo {
			tracked, gitErr := git(root, "ls-files", "-z")
			if gitErr != nil {
				return fmt.Errorf("inspect tracked files before writing .gitattributes: %w", gitErr)
			}
			if tracked != "" {
				fmt.Fprintf(w, "  skipped %s (tracked files exist; adding text attributes may make existing files appear modified; review and migrate attributes manually)\n", path)
				return nil
			}
		}
		content := begin + "\n" + body + "\n" + end + "\n"
		if dry {
			writeUnifiedDiff(w, ".gitattributes", nil, []byte(content))
		}
		return writeFileSafe(path, content, dry, w)
	}
	if err != nil {
		return err
	}
	text := string(old)
	start := strings.Index(text, begin)
	finish := strings.Index(text, end)
	if start < 0 && finish < 0 {
		fmt.Fprintf(w, "  preserved %s (custom attributes detected; no ai-init managed block)\n", path)
		return nil
	}
	if start < 0 || finish < 0 || finish < start || strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		return fmt.Errorf(".gitattributes managed markers are incomplete or out of order; preserving %s", path)
	}
	finish += len(end)
	updated := text[:start] + begin + "\n" + body + "\n" + end + text[finish:]
	if bytes.Equal(old, []byte(updated)) {
		return writeFileSafe(path, updated, dry, w)
	}
	if gitRepo {
		tracked, gitErr := git(root, "ls-files", "-z")
		if gitErr != nil {
			return fmt.Errorf("inspect tracked files before updating .gitattributes: %w", gitErr)
		}
		if tracked != "" {
			fmt.Fprintf(w, "  preserved %s (tracked files exist; changing text attributes may make existing files appear modified; review and migrate attributes manually)\n", path)
			return nil
		}
	}
	if dry && !bytes.Equal(old, []byte(updated)) {
		writeUnifiedDiff(w, ".gitattributes", old, []byte(updated))
	}
	return writeFileSafe(path, updated, dry, w)
}

func writeMemory(root string, p project, dry bool, w io.Writer) error {
	return writeMemoryWithIndexers(root, p, dry, false, w)
}

func writeMemoryWithIndexers(root string, p project, dry, useIndexers bool, w io.Writer) error {
	inventory, err := indexing.Discover(root, p.Git)
	if err != nil {
		return fmt.Errorf("discover project source files: %w", err)
	}
	branch, head := "unknown", "unknown"
	if p.Git {
		if value, e := git(root, "branch", "--show-current"); e == nil && value != "" {
			branch = value
		}
		if value, e := git(root, "rev-parse", "--short", "HEAD"); e == nil {
			head = value
		}
	}
	inventoryStatus := fmt.Sprintf("%s; showing %d of %d source files", inventory.Source, len(inventory.Files), inventory.Total)
	if inventory.Truncated {
		inventoryStatus += " (list truncated)"
	}
	indexerContext := indexing.IndexerContext{SerenaStatus: "not requested", GraphifyStatus: "not requested"}
	if useIndexers && dry {
		indexerContext.SerenaStatus = "skipped (dry-run; no indexer commands run)"
		indexerContext.GraphifyStatus = "skipped (dry-run; no indexer commands run)"
		fmt.Fprintln(w, "  indexers: skipped (dry-run; no indexer commands run)")
	} else if useIndexers {
		indexerContext = reuseInstalledIndexers(root, nil, nil)
		fmt.Fprintf(w, "  Serena: %s\n  Graphify: %s\n", indexerContext.SerenaStatus, indexerContext.GraphifyStatus)
	}
	items := map[string]string{
		"PROJECT_MAP.md":   fmt.Sprintf("# Project map\n\nThese facts come from repository metadata, source paths, and (when explicitly enabled) safe structural index summaries. Verify them against source before relying on them.\n\n- Name: %s\n- Languages: %s\n- Frameworks: %s\n- Dependency managers: %s\n- Source directories: %s\n- Entry points: %s\n- Source file inventory (%s): %s\n- Graphify structural hubs: %s\n- Build hints: %s\n- Test hints: %s\n", markdownFacts([]string{p.Name}), markdownFacts(p.Languages), markdownFacts(p.Frameworks), markdownFacts(p.DependencyManagers), markdownFacts(p.Sources), markdownFacts(p.EntryPoints), inventoryStatus, markdownFacts(inventory.Files), markdownFacts(indexerContext.GraphifyHubs), markdownFacts(p.Build), markdownFacts(p.Tests)),
		"ARCHITECTURE.md":  fmt.Sprintf("# Architecture context\n\n## Verified repository facts\n- Languages: %s\n- Frameworks: %s\n- Source directories: %s\n- Entry points: %s\n- Platform and deployment evidence: %s\n- Build hints: %s\n- Test hints: %s\n\n## Boundaries\nThe deterministic scan does not infer UI/backend or module ownership boundaries. Treat those as unknown until confirmed in source and project documentation.\n", markdownFacts(p.Languages), markdownFacts(p.Frameworks), markdownFacts(p.Sources), markdownFacts(p.EntryPoints), markdownFacts(p.PlatformRequirements), markdownFacts(p.Build), markdownFacts(p.Tests)),
		"CONVENTIONS.md":   fmt.Sprintf("# Detected conventions\n\nFormatting and lint configuration files: %s. Their contents define the authoritative rules. Git convention files: %s. Dependency managers: %s. Existing agent instruction sources: %s. No additional naming or style conventions are inferred here.\n", markdownFacts(p.Configurations), markdownFacts(p.GitConventions), markdownFacts(p.DependencyManagers), markdownFacts(p.AgentInstructions)),
		"CURRENT_STATE.md": fmt.Sprintf("# Current repository state\n\n- Git repository: %t\n- Current branch: %s\n- HEAD: %s\n- Working tree file changes are not captured in this summary.\n- Detected indexer or MCP configuration: %s\n- Serena indexer: %s\n- Graphify indexer: %s\n", p.Git, markdownFacts([]string{branch}), markdownFacts([]string{head}), markdownFacts(p.Indexers), markdownFacts([]string{indexerContext.SerenaStatus}), markdownFacts([]string{indexerContext.GraphifyStatus})),
		"DECISIONS.md":     "# Project decisions\n\nThe deterministic analyzer does not infer design decisions. Record manually approved decisions below.\n",
	}
	for _, name := range []string{"PROJECT_MAP.md", "ARCHITECTURE.md", "CONVENTIONS.md", "CURRENT_STATE.md", "DECISIONS.md"} {
		if err := writeManagedContext(filepath.Join(root, ".ai", name), items[name], dry, w); err != nil {
			return err
		}
	}
	return nil
}

func writeManagedContext(path, body string, dry bool, w io.Writer) error {
	const begin, end = "<!-- ai-init:begin -->", "<!-- ai-init:end -->"
	old, err := os.ReadFile(path)
	if err == nil {
		text := string(old)
		i := strings.Index(text, begin)
		if i < 0 {
			fmt.Fprintf(w, "  preserved %s (manual context has no ai-init markers)\n", path)
			return nil
		}
		relative := strings.Index(text[i:], end)
		if relative < 0 {
			return fmt.Errorf("managed context block in %s is incomplete; preserving file", path)
		}
		j := i + relative + len(end)
		updated := text[:i] + begin + "\n" + strings.TrimSpace(body) + "\n" + end + text[j:]
		return writeFileSafe(path, updated, dry, w)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return writeFileSafe(path, begin+"\n"+strings.TrimSpace(body)+"\n"+end+"\n", dry, w)
}
func installWorktreeGuide(root string, dry bool, w io.Writer) error {
	content := "# Worktrees\n\nUse `ai-init wt new <task>` to create an isolated task branch. Review changes and run validation before integration. Cleanup refuses to remove unmerged work.\n"
	return writeFileSafe(filepath.Join(root, ".ai-init", "WORKTREES.md"), content, dry, w)
}
func agents(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ai-init agents generate|audit|sync|validate|configure|list")
	}
	if args[0] == "list" {
		for _, adapter := range agentcfg.List() {
			fmt.Fprintf(w, "%-12s %s\n", adapter.Name, adapter.Description)
		}
		return nil
	}
	if args[0] == "configure" {
		fs := flag.NewFlagSet("agents configure", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("path", ".", "project path")
		names := fs.String("adapter", "", "comma-separated adapters to configure")
		dry := fs.Bool("dry-run", false, "preview without writing")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		root, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		if !pathSpecified(args[1:]) {
			root = discoverRoot(root)
		}
		if _, err = os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
			return fmt.Errorf("generate canonical AGENTS.md first: %w", err)
		}
		return agentcfg.Configure(root, splitList(*names), *dry, w)
	}
	if args[0] == "generate" || args[0] == "sync" {
		fs := flag.NewFlagSet("agents "+args[0], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("path", ".", "project path")
		dry := fs.Bool("dry-run", false, "preview without writing")
		diff := fs.Bool("diff", false, "show AGENTS.md changes without writing")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *diff {
			*dry = true
		}
		root, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		if !pathSpecified(args[1:]) {
			root = discoverRoot(root)
		}
		project, err := analyzer.Analyze(root)
		if err != nil {
			return err
		}
		if *diff {
			return writeAgentsDiff(root, project, w)
		}
		return writeAgents(root, project, *dry, w)
	}
	root, _, e := target(args[1:])
	if e != nil {
		return e
	}
	switch args[0] {
	case "audit", "validate":
		report, auditErr := auditAgentsFile(filepath.Join(root, "AGENTS.md"))
		if auditErr != nil {
			return auditErr
		}
		fmt.Fprintln(w, report)
		return nil
	default:
		return fmt.Errorf("unknown agents action %q", args[0])
	}
}
func memory(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ai-init memory generate|update")
	}
	if args[0] != "generate" && args[0] != "update" {
		return fmt.Errorf("unknown memory action %q", args[0])
	}
	fs := flag.NewFlagSet("memory", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", ".", "project path")
	dry := fs.Bool("dry-run", false, "preview without writing")
	useIndexers := fs.Bool("use-indexers", false, "reuse existing Serena or Graphify indexes (explicit opt-in)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	root, err := filepath.Abs(*path)
	if err != nil {
		return err
	}
	if !pathSpecified(args[1:]) {
		root = discoverRoot(root)
	}
	p, err := analyzer.Analyze(root)
	if err != nil {
		return err
	}
	return writeMemoryWithIndexers(root, p, *dry, *useIndexers, w)
}

func ports(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ai-init ports allocate <task> <service>|list|release <task>")
	}
	path := "."
	dry := false
	var positional []string
	for i := 1; i < len(args); i++ {
		if args[i] == "--dry-run" {
			dry = true
		} else if args[i] == "--path" {
			if i+1 >= len(args) {
				return errors.New("--path requires a directory")
			}
			i++
			path = args[i]
		} else if strings.HasPrefix(args[i], "--path=") {
			path = strings.TrimPrefix(args[i], "--path=")
		} else {
			positional = append(positional, args[i])
		}
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !pathSpecified(args[1:]) {
		root = discoverRoot(root)
	}
	switch args[0] {
	case "list":
		registry, err := portalloc.List(root)
		if err != nil {
			return err
		}
		for _, task := range portalloc.SortedTasks(registry) {
			for _, service := range portalloc.SortedServices(registry, task) {
				fmt.Fprintf(w, "%s %s %d\n", task, service, registry.Allocations[task][service])
			}
		}
		return nil
	case "allocate":
		if len(positional) != 2 {
			return errors.New("usage: ai-init ports allocate <task> <service> [--path DIR]")
		}
		var port int
		if dry {
			port, err = portalloc.Preview(root, positional[0], positional[1])
		} else {
			port, err = portalloc.Allocate(root, positional[0], positional[1])
		}
		if err != nil {
			return err
		}
		if dry {
			fmt.Fprintf(w, "Would allocate %s %s %d\n", positional[0], positional[1], port)
			return nil
		}
		fmt.Fprintf(w, "%s %s %d\n", positional[0], positional[1], port)
		return nil
	case "release":
		if len(positional) != 1 {
			return errors.New("usage: ai-init ports release <task> [--path DIR]")
		}
		if dry {
			fmt.Fprintf(w, "Would release port records for %s\n", positional[0])
			return nil
		}
		return portalloc.Release(root, positional[0])
	default:
		return fmt.Errorf("unknown ports action %q", args[0])
	}
}

func hooks(args []string, w io.Writer) error {
	if len(args) == 0 || args[0] != "check-staged" {
		return errors.New("usage: ai-init hooks check-staged [--path DIR]")
	}
	root, rest, err := target(args[1:])
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected hooks check-staged arguments: %s", strings.Join(rest, " "))
	}
	return hookguard.CheckStaged(root, w)
}
func prompt(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("choose agents, worktrees, audit, or config")
	}
	topic := args[0]
	fs := flag.NewFlagSet("prompt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("output", "", "export prompt to a new file")
	dry := fs.Bool("dry-run", false, "preview export without writing")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	file := map[string]string{"agents": "generate-agents.md", "worktrees": "setup-worktrees.md", "audit": "audit-project.md", "config": "improve-existing-config.md"}[topic]
	if file == "" {
		return fmt.Errorf("unknown prompt %q", args[0])
	}
	b, e := promptFiles.ReadFile("prompts/" + file)
	if e != nil {
		return e
	}
	if *output != "" {
		path, err := filepath.Abs(*output)
		if err != nil {
			return err
		}
		if *dry {
			fmt.Fprintf(w, "Would export %s to %s\n", topic, path)
			return nil
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return fmt.Errorf("export prompt without overwriting existing files: %w", err)
		}
		if _, err = f.Write(b); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		fmt.Fprintf(w, "Exported %s to %s\n", topic, path)
		return nil
	}
	_, e = w.Write(b)
	return e
}
func doctor(args []string, w io.Writer) error {
	root, _, err := target(args)
	if err != nil {
		return err
	}
	project, err := analyzer.Analyze(root)
	if err != nil {
		return fmt.Errorf("analyze project for environment checks: %w", err)
	}
	fmt.Fprintf(w, "Platform: %s/%s\nGo runtime: %s\nProject: %s\nStack: %s\nGit: %t\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), project.Name, joinOrUnknown(project.Languages), project.Git)
	checks := validation.CheckProjectWithDefaults(root, project)
	if len(checks) == 0 {
		fmt.Fprintln(w, "No project build or test tool requirements were detected.")
		return nil
	}
	var missing []string
	for _, check := range checks {
		if check.Err != nil {
			fmt.Fprintf(w, "Missing: %s (required by %s)\n", check.Command, strings.Join(check.Sources, ", "))
			missing = append(missing, check.Command)
			continue
		}
		fmt.Fprintf(w, "Available: %s (%s)\n", check.Command, check.Path)
	}
	if len(missing) > 0 {
		return fmt.Errorf("environment validation failed; install missing tool(s): %s", strings.Join(missing, ", "))
	}
	fmt.Fprintf(w, "Environment validation: passed (%d required tool(s))\n", len(checks))
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

func validTaskName(task string) bool {
	return taskNamePattern.MatchString(task)
}

var taskNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func integrate(root, sourceWorktree, task string, out io.Writer, dry bool) error {
	clean, err := git(root, "status", "--porcelain")
	if err != nil {
		return err
	}
	if clean != "" {
		return errors.New("target worktree has uncommitted changes; commit or stash them before integration")
	}
	clean, err = git(sourceWorktree, "status", "--porcelain")
	if err != nil {
		return err
	}
	if clean != "" {
		return errors.New("task worktree has uncommitted changes; commit or stash them before integration")
	}
	taskBranch, err := git(sourceWorktree, "branch", "--show-current")
	if err != nil {
		return err
	}
	if taskBranch == "" {
		return errors.New("task worktree is detached; integration requires its task branch")
	}
	if taskBranch != "agent/"+task {
		return fmt.Errorf("task %q resolves to branch %q; expected agent/%s", task, taskBranch, task)
	}
	targetBranch, err := git(root, "branch", "--show-current")
	if err != nil {
		return err
	}
	if targetBranch == "" {
		return errors.New("target worktree is detached; integration requires a checked-out target branch")
	}
	gitPath, err := git(root, "rev-parse", "--git-path", "ai-init-integration.lock")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(gitPath) {
		gitPath = filepath.Join(root, gitPath)
	}
	if err = os.Mkdir(gitPath, 0700); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("another integration holds %s; if it is stale, inspect and remove the lock directory", gitPath)
		}
		return fmt.Errorf("acquire integration lock: %w", err)
	}
	defer os.Remove(gitPath)
	targetHead, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	taskHead, err := git(sourceWorktree, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	parent := filepath.Dir(root)
	temp := filepath.Join(parent, fmt.Sprintf(".ai-init-integrate-%s-%d", filepath.Base(root), os.Getpid()))
	if _, err = os.Lstat(temp); err == nil {
		return fmt.Errorf("disposable integration path already exists: %s", temp)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err = git(root, "worktree", "add", "--detach", temp, targetHead); err != nil {
		return fmt.Errorf("create disposable integration worktree: %w", err)
	}
	registered := true
	defer func() {
		if !registered {
			return
		}
		if _, e := git(temp, "merge", "--abort"); e != nil { /* no merge in progress */
		}
		if _, e := git(root, "worktree", "remove", temp); e != nil {
			fmt.Fprintf(out, "Recovery needed: disposable worktree remains at %s (%v)\n", temp, e)
		}
	}()
	if _, err = git(temp, "merge", "--no-edit", taskHead); err != nil {
		return fmt.Errorf("integration conflict or merge failure in disposable worktree %s; target and task branches are unchanged: %w", temp, err)
	}
	p, err := analyzer.Analyze(temp)
	if err != nil {
		return fmt.Errorf("analyze combined integration result: %w", err)
	}
	if len(p.Tests) == 0 || p.Tests[0] == "unknown" {
		return errors.New("no verified test command was detected; target was not changed")
	}
	for _, test := range p.Tests {
		if test == "unknown (pyproject.toml detected)" {
			continue
		}
		fmt.Fprintf(out, "Running combined-result validation: %s\n", test)
		if err = runValidationCommand(temp, test, out, out); err != nil {
			return fmt.Errorf("combined-result validation failed in disposable worktree; target was not changed: %w", err)
		}
	}
	current, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if current != targetHead {
		return errors.New("target branch changed during validation; integration cancelled")
	}
	current, err = git(root, "branch", "--show-current")
	if err != nil {
		return err
	}
	if current != targetBranch {
		return errors.New("target checkout changed branches during validation; integration cancelled")
	}
	current, err = git(sourceWorktree, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if current != taskHead {
		return errors.New("task branch changed during validation; integration cancelled")
	}
	current, err = git(sourceWorktree, "status", "--porcelain")
	if err != nil {
		return err
	}
	if current != "" {
		return errors.New("task worktree became dirty during validation; integration cancelled")
	}
	clean, err = git(root, "status", "--porcelain")
	if err != nil {
		return err
	}
	if clean != "" {
		return errors.New("target worktree became dirty during validation; integration cancelled")
	}
	if dry {
		if _, err = git(root, "worktree", "remove", temp); err != nil {
			return fmt.Errorf("validation passed, but disposable worktree cleanup failed at %s: %w", temp, err)
		}
		registered = false
		fmt.Fprintf(out, "Validation passed; would integrate %s into %s\n", taskBranch, targetBranch)
		return nil
	}
	if _, err = git(root, "merge", "--ff-only", taskHead); err != nil {
		return fmt.Errorf("validated result could not be fast-forwarded; task and target branches remain available: %w", err)
	}
	if _, err = git(root, "worktree", "remove", temp); err != nil {
		return fmt.Errorf("integration succeeded, but disposable worktree cleanup failed at %s: %w", temp, err)
	}
	registered = false
	fmt.Fprintf(out, "Integrated %s into %s after combined-result validation\n", taskBranch, targetBranch)
	if err = portalloc.Release(root, task); err != nil {
		fmt.Fprintf(out, "Could not release local port records for %s: %v\n", task, err)
	}
	return nil
}

func previewSync(root, taskWorktree, targetBranch string, out io.Writer) error {
	taskHead, err := git(taskWorktree, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	parent := filepath.Dir(root)
	temp := filepath.Join(parent, fmt.Sprintf(".ai-init-sync-%s-%d-%d", filepath.Base(root), os.Getpid(), time.Now().UnixNano()))
	if _, err = git(root, "worktree", "add", "--detach", temp, taskHead); err != nil {
		return fmt.Errorf("create disposable sync preview: %w", err)
	}
	defer func() {
		if _, abortErr := git(temp, "merge", "--abort"); abortErr != nil {
			// No merge may have started, or it may already be up to date.
		}
		if _, removeErr := git(root, "worktree", "remove", "--force", temp); removeErr != nil {
			fmt.Fprintf(out, "Recovery needed: remove disposable sync preview %s manually: %v\n", temp, removeErr)
		}
	}()
	if _, err = git(temp, "merge", "--no-commit", "--no-ff", targetBranch); err != nil {
		return fmt.Errorf("sync preview found a conflict; task and target branches are unchanged: %w", err)
	}
	fmt.Fprintf(out, "Sync preview passed; %s can merge into the task worktree without conflicts\n", targetBranch)
	return nil
}

func worktrees(args []string, w io.Writer) error {
	return worktreesWithOpener(args, w, openWorktree)
}

func worktreesWithOpener(args []string, w io.Writer, open func(root, task, adapter string, out io.Writer) error) error {
	if len(args) == 0 {
		return errors.New("usage: ai-init wt new|open|list|status|sync|integrate|cleanup [task]")
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
	if action == "open" {
		if len(args) < 2 {
			return errors.New("usage: ai-init wt open <task> --ide <adapter>")
		}
		task := args[1]
		if !validTaskName(task) {
			return errors.New("task name must be a single directory name")
		}
		ide := ""
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--ide":
				if i+1 >= len(args) {
					return errors.New("--ide requires an adapter name")
				}
				if ide != "" {
					return errors.New("--ide may be specified only once")
				}
				i++
				ide = strings.ToLower(args[i])
			default:
				return fmt.Errorf("unknown wt open option %q", args[i])
			}
		}
		if ide == "" {
			return errors.New("wt open requires --ide; run 'ai-init agents list' to see configured adapters")
		}
		if !slices.Contains(agentcfg.Names(), ide) {
			return fmt.Errorf("unsupported adapter %q; run 'ai-init agents list'", ide)
		}
		return open(root, task, ide, w)
	}
	if action == "list" {
		value, err := git(root, "worktree", "list", "--porcelain")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, value)
		return nil
	}
	if action == "status" && len(args) == 1 {
		list, listErr := git(root, "worktree", "list", "--porcelain")
		if listErr != nil {
			return listErr
		}
		for _, block := range strings.Split(list, "\n\n") {
			var worktree string
			for _, line := range strings.Split(block, "\n") {
				if strings.HasPrefix(line, "worktree ") {
					worktree = strings.TrimPrefix(line, "worktree ")
					break
				}
			}
			if worktree == "" {
				continue
			}
			state, statusErr := git(worktree, "status", "--short", "--branch")
			if statusErr != nil {
				return statusErr
			}
			fmt.Fprintf(w, "%s\n%s\n", worktree, state)
		}
		return nil
	}
	if action == "new" {
		if len(args) < 2 {
			return errors.New("usage: ai-init wt new <task>")
		}
		task := args[1]
		if !validTaskName(task) {
			return errors.New("task name must be a single directory name")
		}
		parent := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--worktree-parent" {
				if i+1 >= len(args) {
					return errors.New("--worktree-parent requires a directory")
				}
				parent = args[i+1]
				i++
			}
		}
		if parent == "" {
			parent = filepath.Dir(root)
		}
		dest := filepath.Join(parent, filepath.Base(root)+"-worktrees", task)
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("worktree path already exists: %s", dest)
		}
		ide := ""
		dry := false
		var services []string
		var environmentKeys []string
		var isolatePostgresEnv, postgresAdminEnv string
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--ide":
				if i+1 >= len(args) {
					return errors.New("--ide requires an adapter name")
				}
				i++
				ide = args[i]
			case "--port":
				if i+1 >= len(args) {
					return errors.New("--port requires a service name")
				}
				i++
				services = append(services, args[i])
			case "--env":
				if i+1 >= len(args) {
					return errors.New("--env requires an environment variable name")
				}
				i++
				environmentKeys = append(environmentKeys, args[i])
			case "--isolate-postgres-env":
				if i+1 >= len(args) || isolatePostgresEnv != "" {
					return errors.New("--isolate-postgres-env requires one environment variable name")
				}
				i++
				isolatePostgresEnv = args[i]
			case "--postgres-admin-env":
				if i+1 >= len(args) || postgresAdminEnv != "" {
					return errors.New("--postgres-admin-env requires one environment variable name")
				}
				i++
				postgresAdminEnv = args[i]
			case "--dry-run":
				dry = true
			case "--worktree-parent":
				if i+1 >= len(args) {
					return errors.New("--worktree-parent requires a directory")
				}
				i++
			default:
				return fmt.Errorf("unknown wt new option %q", args[i])
			}
		}
		if (isolatePostgresEnv == "") != (postgresAdminEnv == "") {
			return errors.New("--isolate-postgres-env and --postgres-admin-env must be used together")
		}
		if isolatePostgresEnv != "" {
			if strings.EqualFold(isolatePostgresEnv, postgresAdminEnv) {
				return errors.New("target and administrator environment variables must be different")
			}
			for _, key := range environmentKeys {
				if strings.EqualFold(key, postgresAdminEnv) {
					return errors.New("administrator credentials cannot be copied into a worktree environment snapshot")
				}
			}
			var normalizedKeys []string
			for _, key := range environmentKeys {
				if !strings.EqualFold(key, isolatePostgresEnv) {
					normalizedKeys = append(normalizedKeys, key)
				}
			}
			environmentKeys = append(normalizedKeys, isolatePostgresEnv)
			if _, err := exec.LookPath("createdb"); err != nil {
				return errors.New("PostgreSQL createdb is required for --isolate-postgres-env")
			}
			if _, err := exec.LookPath("dropdb"); err != nil {
				return errors.New("PostgreSQL dropdb is required for --isolate-postgres-env and safe cleanup")
			}
		}
		if ide != "" && !slices.Contains(agentcfg.Names(), strings.ToLower(ide)) {
			return fmt.Errorf("unsupported adapter %q; run 'ai-init agents list'", ide)
		}
		ide = strings.ToLower(ide)
		environmentValues, orderedEnvironmentKeys, environmentErr := envprop.CaptureProcess(environmentKeys)
		if environmentErr != nil {
			return environmentErr
		}
		databaseKeys := envprop.DatabaseConnectionKeys(orderedEnvironmentKeys)
		var isolatedDatabaseName string
		var isolatedAdmin postgres.Connection
		var databaseMetadata postgres.Metadata
		if isolatePostgresEnv != "" {
			adminValues, _, captureErr := envprop.CaptureProcess([]string{postgresAdminEnv})
			if captureErr != nil {
				return captureErr
			}
			targetURL, _, adminConn, parseErr := postgres.ParsePair(environmentValues[isolatePostgresEnv], adminValues[postgresAdminEnv])
			if parseErr != nil {
				return parseErr
			}
			adminConn.OmitEnvironment = []string{postgresAdminEnv, isolatePostgresEnv}
			isolatedDatabaseName, err = postgres.NewDatabaseName(task)
			if err != nil {
				return err
			}
			isolatedAdmin = adminConn
			databaseMetadata = postgres.Fingerprint(adminConn)
			databaseMetadata.Task = task
			databaseMetadata.Database = isolatedDatabaseName
			databaseMetadata.Environment = isolatePostgresEnv
			environmentValues[isolatePostgresEnv] = postgres.IsolateURL(targetURL, isolatedDatabaseName)
			var remaining []string
			for _, key := range databaseKeys {
				if !strings.EqualFold(key, isolatePostgresEnv) {
					remaining = append(remaining, key)
				}
			}
			databaseKeys = remaining
			fmt.Fprintf(w, "PostgreSQL isolation requested for %s: task database %s will be provisioned on the explicitly selected server; admin credentials will not be copied or displayed.\n", isolatePostgresEnv, isolatedDatabaseName)
		}
		if len(databaseKeys) > 0 {
			fmt.Fprintf(w, "Warning: selected database-related variables (%s) may point to shared data services. ai-init does not isolate databases; do not run migrations, reset/seed/drop operations, or write shared data without explicit authorization.\n", strings.Join(databaseKeys, ", "))
		}
		branch := "agent/" + task
		if existing, _ := git(root, "branch", "--list", branch); existing != "" {
			return fmt.Errorf("branch already exists: %s", branch)
		}
		if dry {
			for _, service := range services {
				port, previewErr := portalloc.Preview(root, task, service)
				if previewErr != nil {
					return previewErr
				}
				fmt.Fprintf(w, "Would allocate %s port %d\n", service, port)
			}
			fmt.Fprintf(w, "Would create worktree %s on branch %s\n", dest, branch)
			if len(orderedEnvironmentKeys) > 0 {
				fmt.Fprintf(w, "Would copy explicitly selected environment variables %s to an ignored task-local JSON file; values are not displayed\n", strings.Join(orderedEnvironmentKeys, ", "))
			}
			if isolatePostgresEnv != "" {
				fmt.Fprintf(w, "Would create isolated PostgreSQL database %s and set %s to its task-local URL\n", isolatedDatabaseName, isolatePostgresEnv)
			}
			if ide != "" {
				fmt.Fprintf(w, "Would configure %s instructions\n", ide)
				if _, _, launchErr := agentcfg.LaunchCommand(ide); launchErr != nil {
					fmt.Fprintf(w, "No launcher available for %s; worktree will need to be opened manually\n", ide)
				} else {
					fmt.Fprintf(w, "Would open %s in the created worktree\n", ide)
				}
			}
			return nil
		}
		unlockCreation, lockErr := acquireWorktreeCreationLock(root)
		if lockErr != nil {
			return lockErr
		}
		var releaseOnce sync.Once
		releaseCreationLock := func() { releaseOnce.Do(unlockCreation) }
		defer releaseCreationLock()
		if _, statErr := os.Lstat(dest); statErr == nil {
			return fmt.Errorf("worktree path already exists: %s", dest)
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if existing, branchErr := git(root, "branch", "--list", branch); branchErr != nil {
			return branchErr
		} else if existing != "" {
			return fmt.Errorf("branch already exists: %s", branch)
		}
		if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("create worktree parent: %w", err)
		}
		registryBefore, err := portalloc.List(root)
		if err != nil {
			return err
		}
		priorAllocations := registryBefore.Allocations[task]
		allocated := map[string]int{}
		var newlyAllocated []string
		for _, service := range services {
			port, allocErr := portalloc.Allocate(root, task, service)
			if allocErr != nil {
				for _, created := range newlyAllocated {
					_ = portalloc.ReleaseService(root, task, created)
				}
				return allocErr
			}
			allocated[service] = port
			if priorAllocations[service] == 0 {
				newlyAllocated = append(newlyAllocated, service)
			}
		}
		if _, err = git(root, "worktree", "add", "-b", branch, dest); err != nil {
			for _, created := range newlyAllocated {
				_ = portalloc.ReleaseService(root, task, created)
			}
			return err
		}
		worktreeCreated := true
		committed := false
		exclude := ""
		excludeEntryAdded := false
		envExcludeEntryAdded := false
		databaseProvisionAttempted := false
		databaseMetadataWritten := false
		databaseMetadataPath := ""
		defer func() {
			if committed {
				return
			}
			if worktreeCreated {
				if _, cleanupErr := git(root, "worktree", "remove", "--force", dest); cleanupErr != nil {
					fmt.Fprintf(w, "Recovery needed: remove failed worktree %s manually: %v\n", dest, cleanupErr)
				} else {
					worktreeCreated = false
				}
			}
			if !worktreeCreated {
				if _, cleanupErr := git(root, "branch", "-D", branch); cleanupErr != nil {
					fmt.Fprintf(w, "Recovery needed: delete failed task branch %s manually: %v\n", branch, cleanupErr)
				}
			}
			for _, created := range newlyAllocated {
				if cleanupErr := portalloc.ReleaseService(root, task, created); cleanupErr != nil {
					fmt.Fprintf(w, "Recovery needed: release port allocation %s/%s manually: %v\n", task, created, cleanupErr)
				}
			}
			if excludeEntryAdded && exclude != "" {
				if cleanupErr := removeMarkedIgnore(exclude, "# ai-init task-local primer", ".agent-task.md"); cleanupErr != nil {
					fmt.Fprintf(w, "Recovery needed: remove ai-init task primer ignore entry from %s manually: %v\n", exclude, cleanupErr)
				}
			}
			if envExcludeEntryAdded && exclude != "" {
				if cleanupErr := removeMarkedIgnore(exclude, "# ai-init local environment snapshot", ".ai-init.env.json"); cleanupErr != nil {
					fmt.Fprintf(w, "Recovery needed: remove ai-init environment ignore entry from %s manually: %v\n", exclude, cleanupErr)
				}
			}
			removeDatabaseMetadata := databaseMetadataWritten
			if databaseProvisionAttempted {
				cleanupContext, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
				cleanupErr := postgres.Drop(cleanupContext, isolatedAdmin, isolatedDatabaseName, runPostgresClient)
				cancel()
				if cleanupErr != nil {
					removeDatabaseMetadata = false
					fmt.Fprintf(w, "Recovery needed: inspect/drop isolated database %s using environment variable %s; automatic rollback failed.\n", isolatedDatabaseName, postgresAdminEnv)
				}
			}
			if removeDatabaseMetadata {
				if cleanupErr := os.Remove(databaseMetadataPath); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
					fmt.Fprintf(w, "Recovery needed: remove isolated database record %s manually: %v\n", databaseMetadataPath, cleanupErr)
				}
			}
		}()
		if isolatePostgresEnv != "" {
			databaseMetadataPath, err = taskDatabaseMetadataPath(root, task)
			if err != nil {
				return err
			}
			if _, statErr := os.Lstat(databaseMetadataPath); statErr == nil {
				return errors.New("task already has isolated database metadata; inspect it before reusing this task name")
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
			if err = os.MkdirAll(filepath.Dir(databaseMetadataPath), 0700); err != nil {
				return fmt.Errorf("create task database metadata directory: %w", err)
			}
			metadataBytes, marshalErr := json.MarshalIndent(databaseMetadata, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			metadataFile, writeErr := os.OpenFile(databaseMetadataPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if writeErr != nil {
				return fmt.Errorf("write task database metadata: %w", writeErr)
			}
			if _, writeErr = metadataFile.Write(append(metadataBytes, '\n')); writeErr == nil {
				writeErr = metadataFile.Sync()
			}
			closeErr := metadataFile.Close()
			if writeErr == nil {
				writeErr = closeErr
			}
			if writeErr != nil {
				_ = os.Remove(databaseMetadataPath)
				return fmt.Errorf("write task database metadata: %w", writeErr)
			}
			databaseMetadataWritten = true
			databaseProvisionAttempted = true
			createContext, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
			createErr := postgres.Create(createContext, isolatedAdmin, isolatedDatabaseName, runPostgresClient)
			cancel()
			if createErr != nil {
				databaseProvisionAttempted = false
				databaseMetadataWritten = false // Keep the recovery record; the server may have created the database before the client failed.
				fmt.Fprintf(w, "Recovery needed: PostgreSQL creation returned an error. Inspect database %s on the selected server before retrying; metadata is at %s. No automatic drop was attempted.\n", isolatedDatabaseName, databaseMetadataPath)
				return createErr
			}
		}
		if len(environmentValues) > 0 {
			envPath := filepath.Join(dest, ".ai-init.env.json")
			if err = envprop.WriteSnapshot(envPath, environmentValues); err != nil {
				return fmt.Errorf("create explicit task environment snapshot: %w", err)
			}
			fmt.Fprintf(w, "Explicitly selected environment values copied for %s (sensitive values are not displayed)\n", strings.Join(orderedEnvironmentKeys, ", "))
		}
		p, analyzeErr := analyzer.Analyze(dest)
		if analyzeErr != nil {
			return fmt.Errorf("analyze created worktree: %w", analyzeErr)
		}
		if ide != "" {
			if err = writeAgents(dest, p, false, w); err != nil {
				return fmt.Errorf("worktree created but canonical instructions could not be prepared: %w", err)
			}
			if err = agentcfg.Configure(dest, []string{ide}, false, w); err != nil {
				return fmt.Errorf("worktree created but adapter setup failed: %w", err)
			}
		}
		exclude, err = git(root, "rev-parse", "--git-path", "info/exclude")
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
		if !hasIgnorePattern(ignore, ".agent-task.md") {
			if err = appendIgnoreBlock(exclude, "# ai-init task-local primer", ".agent-task.md"); err != nil {
				return err
			}
			excludeEntryAdded = true
			ignore = append(ignore, []byte("\n# ai-init task-local primer\n.agent-task.md\n")...)
		}
		if len(environmentValues) > 0 && !hasIgnorePattern(ignore, ".ai-init.env.json") {
			if err = appendIgnoreBlock(exclude, "# ai-init local environment snapshot", ".ai-init.env.json"); err != nil {
				return err
			}
			envExcludeEntryAdded = true
		}
		var portInfo strings.Builder
		for service, port := range allocated {
			variable := strings.ToUpper(strings.ReplaceAll(service, "-", "_"))
			fmt.Fprintf(&portInfo, "- %s: %d (set AI_INIT_PORT_%s in the service environment)\n", service, port, variable)
		}
		if portInfo.Len() == 0 {
			portInfo.WriteString("- None allocated; use `ai-init wt new <task> --port <service>` when a project needs a dedicated port.\n")
		}
		environmentInfo := "No environment variables were propagated. Do not copy `.env` files or secrets."
		if len(orderedEnvironmentKeys) > 0 {
			environmentInfo = fmt.Sprintf("An explicit allowlist was copied to ignored `.ai-init.env.json`: %s. Its values may be sensitive; load only what the task requires and never commit the file.", strings.Join(orderedEnvironmentKeys, ", "))
		}
		if len(databaseKeys) > 0 {
			environmentInfo += fmt.Sprintf("\nDatabase safety: %s may contain shared database connections. ai-init does not isolate databases. Do not migrate, reset, seed, drop, or write shared data without explicit authorization; configure an isolated database and replace the task-local connection values before database-changing tests.", strings.Join(databaseKeys, ", "))
		}
		if isolatePostgresEnv != "" {
			environmentInfo += fmt.Sprintf("\nPostgreSQL isolation: %s was rewritten to task database %s on the explicitly selected server. The administrator URL was not copied. Keep database-changing tests scoped to this database.", isolatePostgresEnv, isolatedDatabaseName)
		}
		primer := fmt.Sprintf("# Task workspace\n\n- Task: %s\n- Workspace: %s\n- Branch: %s\n- Instructions: `AGENTS.md`\n- Architecture references: `README.md` and existing `docs/` files when present\n- Verification hints: %s\n\n## Port allocations\n%s\n## Environment\n%s\n\nDo not edit another worktree. Preserve unrelated changes. Report files changed and verification performed.\n", task, dest, branch, joinOrUnknown(p.Tests), portInfo.String(), environmentInfo)
		if err = os.WriteFile(filepath.Join(dest, ".agent-task.md"), []byte(primer), 0644); err != nil {
			return fmt.Errorf("worktree created but task primer failed: %w", err)
		}
		committed = true
		fmt.Fprintf(w, "Created %s on branch %s\n", dest, branch)
		releaseCreationLock()
		if ide != "" {
			if err := open(root, task, ide, w); err != nil {
				return fmt.Errorf("worktree was created at %s, but %w", dest, err)
			}
		}
		return nil
	}
	if len(args) < 2 {
		return fmt.Errorf("%s requires a task name", action)
	}
	task := args[1]
	if !validTaskName(task) {
		return errors.New("task name must be a single directory name")
	}
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
		dry := false
		for _, option := range args[2:] {
			if option == "--dry-run" {
				dry = true
			} else {
				return fmt.Errorf("unknown wt sync option %q", option)
			}
		}
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
		if dry {
			return previewSync(root, dest, branch, w)
		}
		if _, err = git(dest, "merge", "--no-edit", branch); err != nil {
			return fmt.Errorf("sync failed; resolve conflicts in the task worktree: %w", err)
		}
		fmt.Fprintf(w, "Synced %s into task worktree\n", branch)
		return nil
	case "integrate":
		dry := false
		for _, option := range args[2:] {
			if option == "--dry-run" {
				dry = true
			} else {
				return fmt.Errorf("unknown wt integrate option %q", option)
			}
		}
		return integrate(root, dest, task, w, dry)
	case "cleanup":
		dry := false
		dropDatabase := false
		adminEnvKey := ""
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--dry-run":
				dry = true
			case "--drop-database":
				dropDatabase = true
			case "--postgres-admin-env":
				if i+1 >= len(args) || adminEnvKey != "" {
					return errors.New("--postgres-admin-env may be specified only once")
				}
				i++
				adminEnvKey = args[i]
			default:
				return fmt.Errorf("unknown wt cleanup option %q", args[i])
			}
		}
		if dropDatabase != (adminEnvKey != "") {
			return errors.New("--drop-database and --postgres-admin-env must be used together")
		}
		metadataPath, pathErr := taskDatabaseMetadataPath(root, task)
		if pathErr != nil {
			return pathErr
		}
		metadataInfo, metadataErr := os.Lstat(metadataPath)
		var databaseMetadata postgres.Metadata
		hasDatabase := metadataErr == nil
		if metadataErr != nil && !os.IsNotExist(metadataErr) {
			return fmt.Errorf("inspect task database metadata: %w", metadataErr)
		}
		if hasDatabase && !metadataInfo.Mode().IsRegular() {
			return errors.New("task database metadata is not a regular file; refusing cleanup")
		}
		var metadataBytes []byte
		if hasDatabase {
			metadataBytes, metadataErr = os.ReadFile(metadataPath)
			if metadataErr != nil {
				return fmt.Errorf("read task database metadata: %w", metadataErr)
			}
			if !dropDatabase {
				return errors.New("worktree owns an isolated database; cleanup requires --drop-database --postgres-admin-env NAME")
			}
			if err = json.Unmarshal(metadataBytes, &databaseMetadata); err != nil {
				return errors.New("task database metadata is invalid; refusing cleanup")
			}
			if err = postgres.ValidateMetadata(databaseMetadata, task); err != nil {
				return err
			}
		} else if dropDatabase {
			return errors.New("no isolated database metadata exists for this worktree")
		}
		var cleanupAdmin postgres.Connection
		if hasDatabase {
			adminValues, _, captureErr := envprop.CaptureProcess([]string{adminEnvKey})
			if captureErr != nil {
				return captureErr
			}
			cleanupAdmin, err = postgres.ParseAdminURL(adminValues[adminEnvKey])
			if err != nil {
				return err
			}
			if !strings.EqualFold(cleanupAdmin.Host, databaseMetadata.Host) || cleanupAdmin.Port != databaseMetadata.Port || cleanupAdmin.User != databaseMetadata.User {
				return errors.New("administrator URL does not match the database endpoint recorded for this worktree")
			}
			cleanupAdmin.OmitEnvironment = []string{adminEnvKey, databaseMetadata.Environment}
		}
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
		if dry {
			fmt.Fprintf(w, "Would remove clean, integrated worktree %s\n", dest)
			if hasDatabase {
				fmt.Fprintf(w, "Would drop task database %s using %s\n", databaseMetadata.Database, adminEnvKey)
			}
			return nil
		}
		if hasDatabase {
			cleanupContext, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
			err = postgres.Drop(cleanupContext, cleanupAdmin, databaseMetadata.Database, runPostgresClient)
			cancel()
			if err != nil {
				return err
			}
		}
		if _, err = git(root, "worktree", "remove", dest); err != nil {
			return err
		}
		if hasDatabase {
			if err = os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("worktree and database removed, but task database metadata could not be removed at %s: %w", metadataPath, err)
			}
		}
		if err = portalloc.Release(root, task); err != nil {
			return fmt.Errorf("worktree removed but port records could not be released: %w", err)
		}
		fmt.Fprintf(w, "Removed clean worktree %s\n", dest)
		return nil
	default:
		return fmt.Errorf("unknown worktree action %q", action)
	}
}

func taskDatabaseMetadataPath(root, task string) (string, error) {
	commonDir, err := git(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(root, commonDir)
	}
	return filepath.Join(commonDir, "ai-init-databases", task+".json"), nil
}

func openWorktree(root, task, adapter string, w io.Writer) error {
	return openWorktreeWith(root, task, adapter, w, exec.LookPath, func(cmd *exec.Cmd) error { return cmd.Run() })
}

func openWorktreeWith(root, task, adapter string, w io.Writer, lookPath func(string) (string, error), run func(*exec.Cmd) error) error {
	command, args, err := agentcfg.LaunchCommand(adapter)
	if err != nil {
		return err
	}
	listing, err := git(root, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	var workspace string
	for _, block := range strings.Split(listing, "\n\n") {
		var path, branch string
		for _, rawLine := range strings.Split(block, "\n") {
			line := strings.TrimSpace(rawLine)
			if strings.HasPrefix(line, "worktree ") {
				path = strings.TrimPrefix(line, "worktree ")
			} else if strings.HasPrefix(line, "branch ") {
				branch = strings.TrimPrefix(line, "branch ")
			}
		}
		if branch == "refs/heads/agent/"+task {
			workspace = path
			break
		}
	}
	if workspace == "" {
		return fmt.Errorf("no worktree found for task %q (expected branch agent/%s)", task, task)
	}
	executable, err := lookPath(command)
	if err != nil && runtime.GOOS == "windows" {
		executable, err = lookPath(command + ".ps1")
	}
	if err != nil {
		return fmt.Errorf("cannot launch %s: %q was not found on PATH; install it or open %s manually", adapter, command, workspace)
	}
	cmdArgs := args
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(executable), ".ps1"):
		powershell, lookupErr := lookPath("powershell.exe")
		if lookupErr != nil {
			powershell, lookupErr = lookPath("pwsh.exe")
		}
		if lookupErr != nil {
			return fmt.Errorf("cannot launch %s: PowerShell was not found on PATH", adapter)
		}
		cmdArgs = append([]string{"-NoProfile", "-File", executable}, args...)
		cmd = exec.Command(powershell, cmdArgs...)
	case runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(executable), ".cmd") || strings.EqualFold(filepath.Ext(executable), ".bat")):
		cmdArgs = append([]string{"/d", "/c", "call", executable}, args...)
		cmd = exec.Command("cmd.exe", cmdArgs...)
	default:
		cmd = exec.Command(executable, args...)
	}
	cmd.Dir = workspace
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Fprintf(w, "Opening %s worktree %s\n", adapter, workspace)
	if err := run(cmd); err != nil {
		return fmt.Errorf("launch %s in %s: %w", adapter, workspace, err)
	}
	return nil
}
