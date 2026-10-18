package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	portalloc "github.com/naoufalelbani/init/internal/ports"
)

func TestAnalyzeGoProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "cmd"), 0700); err != nil {
		t.Fatal(err)
	}
	p := analyze(root)
	if !contains(p.Languages, "Go") {
		t.Fatalf("languages=%v", p.Languages)
	}
	if !contains(p.Tests, "go test ./...") {
		t.Fatalf("tests=%v", p.Tests)
	}
	if !contains(p.Sources, "cmd") {
		t.Fatalf("sources=%v", p.Sources)
	}
}
func TestWriteAgentsPreservesCustomInstructions(t *testing.T) {
	root := t.TempDir()
	want := "# custom rule\nkeep this\n"
	path := filepath.Join(root, "AGENTS.md")
	if e := os.WriteFile(path, []byte(want), 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := writeAgents(root, analyze(root), false, &out); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if string(got) != want {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(out.String(), "preserved") {
		t.Fatalf("output=%q", out.String())
	}
}
func TestWriteManagedAgentsIdempotent(t *testing.T) {
	root := t.TempDir()
	p := analyze(root)
	var out bytes.Buffer
	if e := writeAgents(root, p, false, &out); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e := writeAgents(root, p, false, &out); e != nil {
		t.Fatal(e)
	}
	after, e := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if e != nil {
		t.Fatal(e)
	}
	if string(before) != string(after) {
		t.Fatal("generated instructions changed on repeat")
	}
	if !strings.Contains(out.String(), "already current") {
		t.Fatalf("output=%q", out.String())
	}
}
func TestDryRunDoesNotCreate(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	if e := writeAgents(root, analyze(root), true, &out); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(e) {
		t.Fatalf("dry run wrote file: %v", e)
	}
}
func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
func TestManagedBlockPreservesUserText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	original := "<!-- ai-init:begin -->\nold\n<!-- ai-init:end -->\n\n# My rules\nKeep this section.\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeAgents(root, analyze(root), false, &out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "# My rules\nKeep this section.") {
		t.Fatalf("custom text lost: %s", got)
	}
	if strings.Contains(string(got), "\\n") {
		t.Fatal("literal escape sequences in generated instructions")
	}
}

func TestIntegrateTestsCombinedResultBeforeFastForward(t *testing.T) {
	root, source := integrationFixture(t, "integration 日本語")
	if err := os.WriteFile(filepath.Join(source, "feature.go"), []byte("package sample\n\nfunc Feature() bool { return true }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, source, "add", "feature.go")
	runGitTest(t, source, "commit", "-m", "task change")
	sourceHead, err := git(source, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := integrate(root, source, "feature", &output, false); err != nil {
		t.Fatalf("integrate failed: %v\n%s", err, output.String())
	}
	got, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got != sourceHead {
		t.Fatalf("target HEAD=%s, want task HEAD=%s", got, sourceHead)
	}
	if _, err := os.Stat(filepath.Join(root, "feature.go")); err != nil {
		t.Fatalf("integrated file missing: %v", err)
	}
	if !strings.Contains(output.String(), "go test ./...") {
		t.Fatalf("validation not reported: %s", output.String())
	}
}

func TestIntegrateValidationFailureLeavesBranchesUntouched(t *testing.T) {
	root, source := integrationFixture(t, "integration failure")
	if err := os.WriteFile(filepath.Join(source, "value.go"), []byte("package sample\n\nfunc Value() int { return 2 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, source, "add", "value.go")
	runGitTest(t, source, "commit", "-m", "breaking task change")
	targetBefore, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	sourceBefore, err := git(source, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = integrate(root, source, "feature", &output, false); err == nil {
		t.Fatal("expected validation failure")
	}
	targetAfter, _ := git(root, "rev-parse", "HEAD")
	sourceAfter, _ := git(source, "rev-parse", "HEAD")
	if targetAfter != targetBefore {
		t.Fatal("failed integration changed target branch")
	}
	if sourceAfter != sourceBefore {
		t.Fatal("failed integration changed task branch")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(root), ".ai-init-integrate-"+filepath.Base(root)+"-"+strconv.Itoa(os.Getpid()))); !os.IsNotExist(statErr) {
		t.Fatalf("disposable worktree was not removed: %v", statErr)
	}
}

func integrationFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	parent := t.TempDir()
	root := filepath.Join(parent, name)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "init", "-b", "main")
	runGitTest(t, root, "config", "user.name", "ai-init test")
	runGitTest(t, root, "config", "user.email", "ai-init@example.invalid")
	files := map[string]string{"go.mod": "module example.test/project\n\ngo 1.23\n", "value.go": "package sample\n\nfunc Value() int { return 1 }\n", "value_test.go": "package sample\n\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() != 1 { t.Fatal(Value()) } }\n"}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	source := filepath.Join(parent, name+"-worktree")
	runGitTest(t, root, "worktree", "add", "-b", "agent/feature", source)
	return root, source
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestIntegrateConflictLeavesBothBranchesUntouched(t *testing.T) {
	root, source := integrationFixture(t, "integration conflict")
	if err := os.WriteFile(filepath.Join(root, "value.go"), []byte("package sample\n\nfunc Value() int { return 3 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "value.go")
	runGitTest(t, root, "commit", "-m", "target edit")
	if err := os.WriteFile(filepath.Join(source, "value.go"), []byte("package sample\n\nfunc Value() int { return 4 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, source, "add", "value.go")
	runGitTest(t, source, "commit", "-m", "task edit")
	targetBefore, _ := git(root, "rev-parse", "HEAD")
	sourceBefore, _ := git(source, "rev-parse", "HEAD")
	var output bytes.Buffer
	if err := integrate(root, source, "feature", &output, false); err == nil {
		t.Fatal("expected merge conflict")
	}
	targetAfter, _ := git(root, "rev-parse", "HEAD")
	sourceAfter, _ := git(source, "rev-parse", "HEAD")
	if targetAfter != targetBefore || sourceAfter != sourceBefore {
		t.Fatal("conflict changed an original branch")
	}
}

func TestIntegrateHonorsExistingLock(t *testing.T) {
	root, source := integrationFixture(t, "integration lock")
	lock, err := git(root, "rev-parse", "--git-path", "ai-init-integration.lock")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(lock) {
		lock = filepath.Join(root, lock)
	}
	if err = os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = integrate(root, source, "feature", &output, false); err == nil || !strings.Contains(err.Error(), "holds") {
		t.Fatalf("expected lock error, got %v", err)
	}
	if _, err = os.Stat(lock); err != nil {
		t.Fatalf("existing lock was removed: %v", err)
	}
}

func TestGeneratedAgentsUsesUniversalTemplateAndFacts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/template\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := analyze(root)
	generated := agentsTemplate(p)
	for _, want := range []string{"Respect the existing architecture", "Context efficiency", "Multi-agent development", "Project: " + p.Name, "Detected languages: Go", "Build commands: go build ./...", "{{PROJECT_NAME}}"} {
		if want == "{{PROJECT_NAME}}" {
			if strings.Contains(generated, want) {
				t.Fatal("unexpanded template variable")
			}
			continue
		}
		if !strings.Contains(generated, want) {
			t.Fatalf("generated template missing %q", want)
		}
	}
}

func TestMemoryGeneratesEvidenceFilesAndPreservesManualNotes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/context\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".editorconfig"), []byte("root=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".serena"), 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeMemory(root, analyze(root), false, &output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"PROJECT_MAP.md", "ARCHITECTURE.md", "CONVENTIONS.md", "CURRENT_STATE.md", "DECISIONS.md"} {
		if _, err := os.Stat(filepath.Join(root, ".ai", name)); err != nil {
			t.Fatalf("%s not generated: %v", name, err)
		}
	}
	path := filepath.Join(root, ".ai", "DECISIONS.md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n## Approved decision\nKeep this note.\n")
	_ = f.Close()
	if err := writeMemory(root, analyze(root), false, &output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Keep this note.") {
		t.Fatal("manual decision was lost")
	}
	current, err := os.ReadFile(filepath.Join(root, ".ai", "CURRENT_STATE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), ".serena") {
		t.Fatalf("indexer evidence missing: %s", current)
	}
}

func TestPromptExportCreatesNewFileAndPreservesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standalone prompt.md")
	var output bytes.Buffer
	if err := prompt([]string{"agents", "--output", path}, &output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Inspect the repository before writing") {
		t.Fatalf("unexpected prompt: %s", data)
	}
	if err = prompt([]string{"agents", "--output", path}, &output); err == nil {
		t.Fatal("expected existing file to be preserved")
	}
}
func TestPromptDryRunDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	var output bytes.Buffer
	if err := prompt([]string{"worktrees", "--output", path, "--dry-run"}, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote output: %v", err)
	}
}

func TestDiscoverRootFromNestedPath(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "packages", "service")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname='x'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := discoverRoot(nested)
	if got != root {
		t.Fatalf("root=%s, want %s", got, root)
	}
}

func TestSetupEmptyUnicodeProjectIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project 空 dir")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	var first bytes.Buffer
	if err := Run([]string{"setup", "--path", root}, &first, &first); err != nil {
		t.Fatal(err)
	}
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"PROJECT_MAP.md", "ARCHITECTURE.md", "CONVENTIONS.md", "CURRENT_STATE.md", "DECISIONS.md"} {
		if _, err = os.Stat(filepath.Join(root, ".ai", file)); err != nil {
			t.Fatalf("missing %s: %v", file, err)
		}
	}
	var second bytes.Buffer
	if err := Run([]string{"setup", "--path", root}, &second, &second); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(agents) != string(again) {
		t.Fatal("setup was not idempotent")
	}
}

func TestSetupEmptyRepositoryCreatesInstructionsAndContext(t *testing.T) {
	root := filepath.Join(t.TempDir(), "empty repository")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"AGENTS.md", filepath.Join(".ai", "PROJECT_MAP.md"), filepath.Join(".ai", "DECISIONS.md")} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("missing generated artifact %s: %v", path, err)
		}
	}
	if !strings.Contains(output.String(), "Stack: unknown") || !strings.Contains(output.String(), "Validation: completed") {
		t.Fatalf("unexpected setup summary: %s", output.String())
	}
}

func TestSetupRejectsInvalidOptionsBeforeWriting(t *testing.T) {
	for _, args := range [][]string{
		{"setup", "--path", t.TempDir(), "--with", "agents,unknown"},
		{"setup", "--path", t.TempDir(), "--adapters", "unknown"},
	} {
		root := args[2]
		var output bytes.Buffer
		if err := Run(args, &output, &output); err == nil {
			t.Fatalf("expected invalid option error for %v", args)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("invalid options wrote files into %s: %v", root, entries)
		}
	}
}
func TestSetupPreservesExistingProjectInstructions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	want := "# owner instructions\nDo not remove this.\n"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root}, &output, &output); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("custom instructions changed: %q", got)
	}
}

func TestAnalyzeTypeScriptReactProject(t *testing.T) {
	root := t.TempDir()
	manifest := `{"scripts":{"build":"vite build","test":"vitest run"},"dependencies":{"react":"^19","vite":"^7"}}`
	for name, content := range map[string]string{"package.json": manifest, "pnpm-lock.yaml": "lockfileVersion: '9.0'\n", "tsconfig.json": "{}", "src/main.ts": "export {}\n"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := analyze(root)
	for _, want := range []string{"JavaScript", "TypeScript"} {
		if !contains(p.Languages, want) {
			t.Fatalf("languages=%v", p.Languages)
		}
	}
	for _, want := range []string{"react", "vite", "Node.js"} {
		if !contains(p.Frameworks, want) {
			t.Fatalf("frameworks=%v", p.Frameworks)
		}
	}
	for _, want := range []string{"pnpm run build"} {
		if !contains(p.Build, want) {
			t.Fatalf("build=%v", p.Build)
		}
	}
	if !contains(p.Tests, "pnpm test") {
		t.Fatalf("tests=%v", p.Tests)
	}
	if !contains(p.EntryPoints, "src/main.ts") {
		t.Fatalf("entrypoints=%v", p.EntryPoints)
	}
}

func TestWorktreeCreationFailureRollsBackBranchWorktreeAndPorts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "init", "-b", "main")
	runGitTest(t, root, "config", "user.name", "ai-init test")
	runGitTest(t, root, "config", "user.email", "ai-init@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/rollback\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aider.conf.yml"), []byte("# <!-- ai-init adapter:begin -->\ntruncated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitExclude := runGitTest(t, root, "rev-parse", "--git-path", "info/exclude")
	if !filepath.IsAbs(gitExclude) {
		gitExclude = filepath.Join(root, gitExclude)
	}
	if err := os.MkdirAll(filepath.Dir(gitExclude), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitExclude, []byte("# user's ignore rule\n*.local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	destinationParent := filepath.Join(t.TempDir(), "worktree parent")
	t.Chdir(root)
	var output bytes.Buffer
	err := worktrees([]string{"new", "rollback", "--ide", "aider", "--port", "api", "--worktree-parent", destinationParent}, &output)
	if err == nil {
		t.Fatal("expected adapter error")
	}
	if value, gitErr := git(root, "branch", "--list", "agent/rollback"); gitErr != nil || value != "" {
		t.Fatalf("failed branch remains: %q (%v)", value, gitErr)
	}
	dest := filepath.Join(destinationParent, "repo-worktrees", "rollback")
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("failed worktree remains: %v", statErr)
	}
	registry, listErr := portalloc.List(root)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(registry.Allocations["rollback"]) != 0 {
		t.Fatalf("failed creation leaked ports: %v", registry.Allocations)
	}
	ignore, err := os.ReadFile(gitExclude)
	if err != nil {
		t.Fatal(err)
	}
	if string(ignore) != "# user's ignore rule\n*.local\n" {
		t.Fatalf("rollback modified unrelated exclude content: %q", ignore)
	}
}

func TestRunValidationCommandExecutesGoTests(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/validate\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "validate.go"), []byte("package validate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runValidationCommand(dir, "go test ./...", &output, &output); err != nil {
		t.Fatalf("validation command failed: %v\n%s", err, output.String())
	}
}

func TestRunValidationCommandExecutesWindowsWrapperWhenAvailable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command wrapper execution is platform-specific")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.cmd"), []byte("@echo off\r\nexit /b 0\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runValidationCommand(dir, "test.cmd", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrateDryRunValidatesWithoutChangingBranches(t *testing.T) {
	root, source := integrationFixture(t, "integration dry run")
	if err := os.WriteFile(filepath.Join(source, "feature.go"), []byte("package sample\nfunc Feature() bool { return true }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, source, "add", "feature.go")
	runGitTest(t, source, "commit", "-m", "task change")
	targetBefore, _ := git(root, "rev-parse", "HEAD")
	sourceBefore, _ := git(source, "rev-parse", "HEAD")
	var output bytes.Buffer
	if err := integrate(root, source, "feature", &output, true); err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, output.String())
	}
	targetAfter, _ := git(root, "rev-parse", "HEAD")
	sourceAfter, _ := git(source, "rev-parse", "HEAD")
	if targetAfter != targetBefore || sourceAfter != sourceBefore {
		t.Fatal("dry run changed a branch")
	}
	if !strings.Contains(output.String(), "Validation passed; would integrate") {
		t.Fatalf("missing dry-run report: %s", output.String())
	}
}

func TestEmbeddedPromptAssetsMatchStandaloneFiles(t *testing.T) {
	for _, name := range []string{"generate-agents.md", "setup-worktrees.md", "audit-project.md", "improve-existing-config.md"} {
		embedded, err := promptFiles.ReadFile("prompts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		disk, err := os.ReadFile(filepath.Join("..", "..", "prompts", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(embedded, disk) {
			t.Fatalf("embedded prompt %s differs from standalone asset", name)
		}
	}
	embedded, err := promptFiles.ReadFile("templates/AGENTS.template.md")
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join("..", "..", "templates", "AGENTS.template.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, disk) {
		t.Fatal("embedded universal template differs from standalone asset")
	}
}
