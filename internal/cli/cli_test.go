package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/naoufalelbani/init/internal/analyzer"
	"github.com/naoufalelbani/init/internal/indexing"
	portalloc "github.com/naoufalelbani/init/internal/ports"
)

func analyze(root string) project {
	p, err := analyzer.Analyze(root)
	if err != nil {
		panic(err)
	}
	return p
}

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

func TestDoctorChecksDetectedProjectToolsAndReturnsNonzeroWhenMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project with space")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/doctor\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	t.Setenv("PATH", path)
	var output bytes.Buffer
	err := doctor([]string{"--path", root}, &output)
	if err == nil || !strings.Contains(err.Error(), "install missing tool(s): go") {
		t.Fatalf("doctor error=%v", err)
	}
	if !strings.Contains(output.String(), "Project: project with space") || !strings.Contains(output.String(), "Missing: go") {
		t.Fatalf("project-aware missing-tool report absent: %s", output.String())
	}
}

func TestDoctorReportsNoDetectedRequirementsForEmptyProject(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	if err := doctor([]string{"--path", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No project build or test tool requirements were detected") {
		t.Fatalf("unexpected doctor report: %s", output.String())
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

func TestAgentAuditDistinguishesManagedCustomAndInvalidFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# local contract\nDo not rewrite architecture.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := auditAgentsFile(path)
	if err != nil || !strings.Contains(report, "custom instructions") {
		t.Fatalf("custom audit report=%q err=%v", report, err)
	}
	for _, invalid := range []string{"", "<!-- ai-init:end -->\nbody", "<!-- ai-init:begin -->\n", "<!-- ai-init:begin -->\na\n<!-- ai-init:end -->\n<!-- ai-init:begin -->\nb\n<!-- ai-init:end -->"} {
		if err = os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = auditAgentsFile(path); err == nil {
			t.Fatalf("invalid AGENTS.md passed validation: %q", invalid)
		}
	}
	valid := "<!-- ai-init:begin -->\nverified rules\n<!-- ai-init:end -->\n"
	if err = os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	report, err = auditAgentsFile(path)
	if err != nil || !strings.Contains(report, "managed instructions valid") {
		t.Fatalf("managed audit report=%q err=%v", report, err)
	}
}

func TestWriteAgentsPreservesBrokenMarkerPair(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	want := "<!-- ai-init:end -->\ncustom text\n"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeAgents(root, analyze(root), false, &out); err == nil {
		t.Fatal("expected malformed marker error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("malformed instructions changed: %q", got)
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

func TestAgentDiffShowsManagedUpdateWithoutWriting(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/diff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeAgents(root, analyze(root), false, &output); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "AGENTS.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"react":"^19"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err = Run([]string{"agents", "generate", "--path", root, "--diff"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "--- a/AGENTS.md") || !strings.Contains(output.String(), "+- Detected frameworks: `Node.js`, `react`") {
		t.Fatalf("diff missing expected managed update: %s", output.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("diff mode modified AGENTS.md")
	}
}

func TestAgentDiffPreservesCustomInstructionsWithoutSuggestingReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	want := "# owner rules\nKeep these instructions.\n"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"agents", "sync", "--path", root, "--diff"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "custom instructions") || strings.Contains(output.String(), "--- a/AGENTS.md") {
		t.Fatalf("custom file should be reported preserved without replacement diff: %s", output.String())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("custom instructions changed: %q err=%v", got, err)
	}
}

func TestSetupDiffPreviewsWithoutCreatingFiles(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root, "--diff"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "--- /dev/null") || !strings.Contains(output.String(), "Validation: skipped") {
		t.Fatalf("setup diff report incomplete: %s", output.String())
	}
	for _, path := range []string{"AGENTS.md", ".ai", ".ai-init"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("diff mode created %s: %v", path, err)
		}
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
	source := filepath.Join(parent, filepath.Base(root)+"-worktrees", "feature")
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
	if err := os.WriteFile(filepath.Join(root, ".golangci.yml"), []byte("linters:\n  enable: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Use the repository's local rules.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := analyze(root)
	generated := agentsTemplate(p)
	for _, want := range []string{"Respect the existing architecture", "Context efficiency", "Multi-agent development", "Project: `" + p.Name + "`", "Detected languages: `Go`", "Detected dependency managers: `Go modules`", "Existing agent instruction files: `AGENTS.md`", "Formatting and lint configuration files: `.golangci.yml`", "Build commands: `go build ./...`", "{{PROJECT_NAME}}"} {
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

func TestMarkdownFactsCannotCreateNewlinesOrBreakInlineCode(t *testing.T) {
	formatted := markdownFacts([]string{"```unsafe\n- injected rule", "ordinary"})
	if strings.ContainsAny(formatted, "\r\n") {
		t.Fatalf("control characters survived Markdown fact formatting: %q", formatted)
	}
	if !strings.Contains(formatted, "```````unsafe - injected rule````") {
		t.Fatalf("backtick run was not safely delimited: %q", formatted)
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
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\nfunc main() { println(\"private source body\") }\n"), 0600); err != nil {
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
	projectMap, err := os.ReadFile(filepath.Join(root, ".ai", "PROJECT_MAP.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projectMap), "src/main.go") || strings.Contains(string(projectMap), "private source body") {
		t.Fatalf("project map must inventory source paths without copying source content: %s", projectMap)
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
	conventions, err := os.ReadFile(filepath.Join(root, ".ai", "CONVENTIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(conventions), ".editorconfig") || !strings.Contains(string(conventions), "Go modules") {
		t.Fatalf("project configuration or dependency-manager evidence missing: %s", conventions)
	}
}

func TestMemoryIndexerOptInDryRunDoesNotInvokeOrWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".serena"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".serena", "project.yml"), []byte("language: go\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"memory", "generate", "--path", root, "--use-indexers", "--dry-run"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "dry-run; no indexer commands run") {
		t.Fatalf("missing opt-in dry-run report: %s", output.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".ai")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote project context: %v", err)
	}
}

func TestMemoryIncludesOnlyReturnedStructuralIndexerSummary(t *testing.T) {
	root := t.TempDir()
	wantContext := indexing.IndexerContext{
		SerenaStatus:   "symbol index refreshed",
		GraphifyStatus: "existing graph queried; 1 hubs included",
		GraphifyHubs:   []string{"internal/indexing (degree 12)"},
	}
	previous := reuseInstalledIndexers
	reuseInstalledIndexers = func(gotRoot string, _ indexing.LookupFunc, _ indexing.CommandFunc) indexing.IndexerContext {
		if gotRoot != root {
			t.Fatalf("root=%q want=%q", gotRoot, root)
		}
		return wantContext
	}
	t.Cleanup(func() { reuseInstalledIndexers = previous })
	var output bytes.Buffer
	if err := Run([]string{"memory", "generate", "--path", root, "--use-indexers"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	projectMap, err := os.ReadFile(filepath.Join(root, ".ai", "PROJECT_MAP.md"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(root, ".ai", "CURRENT_STATE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projectMap), "internal/indexing (degree 12)") || !strings.Contains(string(current), "symbol index refreshed") || !strings.Contains(output.String(), "existing graph queried; 1 hubs included") {
		t.Fatalf("indexer summary did not reach generated context/report:\n%s\n%s\n%s", projectMap, current, output.String())
	}
}

func TestSetupIndexerOptInRequiresMemory(t *testing.T) {
	var output bytes.Buffer
	err := Run([]string{"setup", "--with", "agents", "--use-indexers"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "requires the memory component") {
		t.Fatalf("expected memory requirement, got %v", err)
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

func TestSetupWithAgentAdaptersIsIdempotent(t *testing.T) {
	root := t.TempDir()
	args := []string{"setup", "--path", root, "--with", "agents,memory", "--adapters", "claude,cursor,copilot,aider,windsurf"}
	var first bytes.Buffer
	if err := Run(args, &first, &first); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"AGENTS.md", "CLAUDE.md", filepath.Join(".cursor", "rules", "ai-init.mdc"),
		filepath.Join(".github", "copilot-instructions.md"), ".aider.conf.yml", filepath.Join(".windsurf", "rules", "ai-init.md"),
		filepath.Join(".ai", "PROJECT_MAP.md"), filepath.Join(".ai", "ARCHITECTURE.md"), filepath.Join(".ai", "CONVENTIONS.md"),
		filepath.Join(".ai", "CURRENT_STATE.md"), filepath.Join(".ai", "DECISIONS.md"),
	}
	before := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read first-run output %s: %v", path, err)
		}
		before[path] = data
	}
	var second bytes.Buffer
	if err := Run(args, &second, &second); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read second-run output %s: %v", path, err)
		}
		if !bytes.Equal(before[path], data) {
			t.Errorf("repeated setup changed generated file %s", path)
		}
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
	if !strings.Contains(output.String(), "Stack: unknown") || !strings.Contains(output.String(), "Validation: passed for selected files") {
		t.Fatalf("unexpected setup summary: %s", output.String())
	}
}

func TestSetupDryRunDoesNotClaimValidationPassed(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root, "--dry-run"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Validation: skipped") || strings.Contains(output.String(), "Validation: passed") {
		t.Fatalf("dry-run validation status is misleading: %s", output.String())
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created instructions: %v", err)
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
	t.Setenv("AI_INIT_TEST_ROLLBACK_SECRET", "rollback secret value")
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
	err := worktrees([]string{"new", "rollback", "--ide", "aider", "--port", "api", "--env", "AI_INIT_TEST_ROLLBACK_SECRET", "--worktree-parent", destinationParent}, &output)
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

func TestWorktreeEnvironmentPropagationRequiresAllowlistAndStaysIgnored(t *testing.T) {
	root, _ := integrationFixture(t, "env propagation")
	t.Setenv("AI_INIT_TEST_TOKEN", "secret value with $ and quotes \" ")
	t.Setenv("DATABASE_URL", "postgres://task-user:p4ss@localhost/shared")
	t.Setenv("AI_INIT_TEST_UNSELECTED", "must not be copied")
	t.Chdir(root)
	var output bytes.Buffer
	if err := worktrees([]string{"new", "env-task", "--env", "AI_INIT_TEST_TOKEN", "--env", "DATABASE_URL"}, &output); err != nil {
		t.Fatalf("create worktree: %v\n%s", err, output.String())
	}
	dest := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", "env-task")
	data, err := os.ReadFile(filepath.Join(dest, ".ai-init.env.json"))
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err = json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values["AI_INIT_TEST_TOKEN"] != `secret value with $ and quotes " ` || values["DATABASE_URL"] != "postgres://task-user:p4ss@localhost/shared" {
		t.Fatalf("unexpected allowlisted environment snapshot: %#v", values)
	}
	primer := string(mustRead(t, filepath.Join(dest, ".agent-task.md")))
	if strings.Contains(output.String(), "secret value") || strings.Contains(output.String(), "p4ss") || strings.Contains(primer, "secret value") || strings.Contains(primer, "p4ss") {
		t.Fatal("secret value leaked into CLI output or task primer")
	}
	if !strings.Contains(primer, "AI_INIT_TEST_TOKEN") {
		t.Fatal("task primer does not describe the explicit allowlist")
	}
	if !strings.Contains(output.String(), "ai-init does not isolate databases") || !strings.Contains(primer, "Do not migrate, reset, seed, drop") {
		t.Fatal("database safety warning missing from output or task primer")
	}
	if _, err = git(dest, "check-ignore", "--", ".ai-init.env.json"); err != nil {
		t.Fatalf("environment snapshot is not ignored: %v", err)
	}
	status, err := git(dest, "status", "--porcelain")
	if err != nil || status != "" {
		t.Fatalf("task-local environment file appears in Git status: %q err=%v", status, err)
	}
}

func TestWorktreeEnvironmentDryRunDoesNotWriteOrPrintValues(t *testing.T) {
	root, _ := integrationFixture(t, "env dry run")
	t.Setenv("AI_INIT_TEST_DRY_SECRET", "dry run secret")
	t.Setenv("DATABASE_URL", "postgres://task-user:dry-secret@localhost/shared")
	t.Chdir(root)
	var output bytes.Buffer
	if err := worktrees([]string{"new", "env-preview", "--env", "AI_INIT_TEST_DRY_SECRET", "--env", "DATABASE_URL", "--dry-run"}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "dry run secret") || strings.Contains(output.String(), "dry-secret") || !strings.Contains(output.String(), "AI_INIT_TEST_DRY_SECRET") || !strings.Contains(output.String(), "ai-init does not isolate databases") {
		t.Fatalf("unsafe or missing dry-run report: %s", output.String())
	}
	if branch, err := git(root, "branch", "--list", "agent/env-preview"); err != nil || branch != "" {
		t.Fatalf("dry-run created branch: %q err=%v", branch, err)
	}
	path := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", "env-preview")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run created worktree: %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWorktreeCreationProcessHelper(t *testing.T) {
	root := os.Getenv("AI_INIT_TEST_WORKTREE_ROOT")
	if root == "" {
		return
	}
	t.Chdir(root)
	var output bytes.Buffer
	task := os.Getenv("AI_INIT_TEST_WORKTREE_TASK")
	parent := os.Getenv("AI_INIT_TEST_WORKTREE_PARENT")
	if err := worktrees([]string{"new", task, "--worktree-parent", parent}, &output); err != nil {
		t.Fatalf("create %s: %v\n%s", task, err, output.String())
	}
}

func TestConcurrentWorktreeCreationUsesDistinctTasksSafely(t *testing.T) {
	root, _ := integrationFixture(t, "parallel worktrees")
	parent := filepath.Join(t.TempDir(), "parallel checkouts")
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, task := range []string{"first", "second"} {
		group.Add(1)
		go func(task string) {
			defer group.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestWorktreeCreationProcessHelper$")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "AI_INIT_TEST_WORKTREE_ROOT="+root, "AI_INIT_TEST_WORKTREE_TASK="+task, "AI_INIT_TEST_WORKTREE_PARENT="+parent)
			if output, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("task %s: %w\n%s", task, err, output)
				return
			}
			errs <- nil
		}(task)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []string{"first", "second"} {
		if _, err := os.Stat(filepath.Join(parent, "parallel worktrees-worktrees", task, ".git")); err != nil {
			t.Fatalf("worktree %s not created: %v", task, err)
		}
		branch, err := git(filepath.Join(parent, "parallel worktrees-worktrees", task), "branch", "--show-current")
		if err != nil || branch != "agent/"+task {
			t.Fatalf("task %s branch=%q err=%v", task, branch, err)
		}
	}
}

func TestWorktreeNewWithIDEOpensOnlyAfterSetupAndLockRelease(t *testing.T) {
	root, _ := integrationFixture(t, "new with IDE")
	t.Chdir(root)
	opened := false
	open := func(gitRoot, task, adapter string, _ io.Writer) error {
		opened = true
		gotRoot, err := filepath.EvalSymlinks(gitRoot)
		if err != nil {
			t.Fatal(err)
		}
		wantRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		rootMatches := filepath.Clean(gotRoot) == filepath.Clean(wantRoot)
		if runtime.GOOS == "windows" {
			rootMatches = strings.EqualFold(filepath.Clean(gotRoot), filepath.Clean(wantRoot))
		}
		if !rootMatches || task != "auto-open" || adapter != "codex" {
			t.Fatalf("opener received root=%q task=%q adapter=%q", gitRoot, task, adapter)
		}
		workspace := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", task)
		if _, err := os.Stat(filepath.Join(workspace, "AGENTS.md")); err != nil {
			t.Fatalf("instructions were not prepared before launch: %v", err)
		}
		if _, err := os.Stat(filepath.Join(workspace, ".agent-task.md")); err != nil {
			t.Fatalf("task primer was not prepared before launch: %v", err)
		}
		lockPath := runGitTest(t, root, "rev-parse", "--git-path", "ai-init-worktree-create.lock")
		if !filepath.IsAbs(lockPath) {
			lockPath = filepath.Join(root, lockPath)
		}
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Fatalf("worktree creation lock still held while launching: %v", err)
		}
		return nil
	}
	var output bytes.Buffer
	if err := worktreesWithOpener([]string{"new", "auto-open", "--ide", "codex"}, &output, open); err != nil {
		t.Fatalf("create and open worktree: %v\n%s", err, output.String())
	}
	if !opened || !strings.Contains(output.String(), "Created ") {
		t.Fatalf("worktree was not reported and opened: %s", output.String())
	}
}

func TestWorktreePostgresIsolationLifecycleUsesTaskDatabaseAndExplicitCleanup(t *testing.T) {
	root, _ := integrationFixture(t, "postgres isolation")
	t.Chdir(root)
	tools := t.TempDir()
	toolSuffix := ""
	if runtime.GOOS == "windows" {
		toolSuffix = ".exe"
	}
	for _, name := range []string{"createdb", "dropdb"} {
		if err := os.WriteFile(filepath.Join(tools, name+toolSuffix), []byte("test stub"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DATABASE_URL", "postgres://app_user:target_secret@db.example:5432/shared")
	t.Setenv("ADMIN_DATABASE_URL", "postgres://db_admin:admin_secret@db.example:5432/postgres")
	previousRunner := runPostgresClient
	t.Cleanup(func() { runPostgresClient = previousRunner })
	var created, dropped int
	var databaseName string
	var createFailure error
	var output bytes.Buffer
	runPostgresClient = func(_ context.Context, command string, args, env []string) error {
		if command == "createdb" {
			created++
		} else if command == "dropdb" {
			dropped++
		} else {
			t.Fatalf("unexpected PostgreSQL command %q", command)
		}
		for _, arg := range args {
			if strings.HasPrefix(arg, "--owner=") && strings.TrimPrefix(arg, "--owner=") != "app_user" {
				t.Fatalf("unexpected database owner flag %q", arg)
			}
			if strings.Contains(arg, "admin_secret") || strings.Contains(arg, "target_secret") {
				t.Fatalf("credential leaked in client argument %q", arg)
			}
		}
		if strings.Contains(strings.Join(env, "\n"), "admin_secret") || strings.Contains(strings.Join(env, "\n"), "target_secret") {
			t.Fatal("credential leaked in PostgreSQL client environment")
		}
		if command == "createdb" {
			for _, arg := range args {
				if strings.HasPrefix(arg, "ai_init_") {
					databaseName = arg
				}
			}
		}
		if command == "createdb" && createFailure != nil {
			return createFailure
		}
		return nil
	}
	if err := worktrees([]string{"new", "isolated-task", "--isolate-postgres-env", "DATABASE_URL", "--postgres-admin-env", "ADMIN_DATABASE_URL"}, &output); err != nil {
		t.Fatalf("create isolated task worktree: %v\n%s", err, output.String())
	}
	if created != 1 || databaseName == "" || dropped != 0 {
		t.Fatalf("create/drop calls = %d/%d, database %q", created, dropped, databaseName)
	}
	workspace := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", "isolated-task")
	snapshot, err := os.ReadFile(filepath.Join(workspace, ".ai-init.env.json"))
	if err != nil {
		t.Fatal(err)
	}
	var environmentValues map[string]string
	if err = json.Unmarshal(snapshot, &environmentValues); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(environmentValues["DATABASE_URL"], "/"+databaseName) || strings.Contains(string(snapshot), "admin_secret") {
		t.Fatalf("unexpected task environment snapshot: %s", snapshot)
	}
	metadataPath, err := taskDatabaseMetadataPath(root, "isolated-task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(metadataPath); err != nil {
		t.Fatalf("task database recovery metadata missing: %v", err)
	}
	if err = worktrees([]string{"cleanup", "isolated-task"}, &output); err == nil || !strings.Contains(err.Error(), "--drop-database") {
		t.Fatalf("cleanup without explicit database drop should refuse, got %v", err)
	}
	if err = worktrees([]string{"cleanup", "isolated-task", "--drop-database", "--postgres-admin-env", "ADMIN_DATABASE_URL", "--dry-run"}, &output); err != nil {
		t.Fatalf("database cleanup dry run: %v", err)
	}
	if dropped != 0 {
		t.Fatal("dry-run dropped the task database")
	}
	if err = worktrees([]string{"cleanup", "isolated-task", "--drop-database", "--postgres-admin-env", "ADMIN_DATABASE_URL"}, &output); err != nil {
		t.Fatalf("database cleanup: %v\n%s", err, output.String())
	}
	if dropped != 1 {
		t.Fatalf("drop calls = %d, want 1", dropped)
	}
	if _, err = os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("database metadata remains after cleanup: %v", err)
	}
	if strings.Contains(output.String(), "admin_secret") || strings.Contains(output.String(), "target_secret") {
		t.Fatal("CLI output exposed a database credential")
	}
	createFailure = fmt.Errorf("simulated client failure")
	if err = worktrees([]string{"new", "uncertain-task", "--isolate-postgres-env", "DATABASE_URL", "--postgres-admin-env", "ADMIN_DATABASE_URL"}, &output); err == nil {
		t.Fatal("expected simulated PostgreSQL creation failure")
	}
	if dropped != 1 {
		t.Fatalf("failed create triggered automatic database drop: %d drop calls", dropped-1)
	}
	uncertainMetadata, pathErr := taskDatabaseMetadataPath(root, "uncertain-task")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, err = os.Stat(uncertainMetadata); err != nil {
		t.Fatalf("ambiguous create failure lost its recovery record: %v", err)
	}
	if err = os.Remove(uncertainMetadata); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeNewUnsupportedIDEReportsLaunchFailureAndRetainsWorktree(t *testing.T) {
	root, _ := integrationFixture(t, "unsupported launch")
	t.Chdir(root)
	var output bytes.Buffer
	err := worktreesWithOpener([]string{"new", "manual-open", "--ide", "antigravity"}, &output, openWorktree)
	if err == nil || !strings.Contains(err.Error(), "not supported") || !strings.Contains(err.Error(), "worktree was created at") {
		t.Fatalf("expected explicit post-creation launch error, got %v", err)
	}
	workspace := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", "manual-open")
	if _, statErr := os.Stat(filepath.Join(workspace, ".git")); statErr != nil {
		t.Fatalf("created worktree was incorrectly removed after unsupported launch: %v", statErr)
	}
	if !strings.Contains(output.String(), "Created ") {
		t.Fatalf("successful worktree creation not reported before launch error: %s", output.String())
	}
}

func TestWorktreeOpenBuildsLaunchInRegisteredPathWithSpaces(t *testing.T) {
	root, _ := integrationFixture(t, "launch workspace")
	t.Chdir(root)
	parent := filepath.Join(filepath.Dir(root), "task checkouts")
	var output bytes.Buffer
	if err := worktrees([]string{"new", "launch-test", "--worktree-parent", parent}, &output); err != nil {
		t.Fatalf("create worktree: %v\n%s", err, output.String())
	}
	workspace := filepath.Join(parent, "launch workspace-worktrees", "launch-test")
	output.Reset()
	var invoked *exec.Cmd
	lookPath := func(command string) (string, error) {
		if command != "codex" {
			return "", fmt.Errorf("unexpected executable lookup %q", command)
		}
		return filepath.Join(t.TempDir(), "fake tools", "codex"), nil
	}
	run := func(cmd *exec.Cmd) error {
		invoked = cmd
		return nil
	}
	if err := openWorktreeWith(root, "launch-test", "codex", &output, lookPath, run); err != nil {
		t.Fatalf("open worktree: %v\n%s", err, output.String())
	}
	if invoked == nil {
		t.Fatal("launcher process was not prepared")
	}
	gotDir, err := filepath.EvalSymlinks(invoked.Dir)
	if err != nil {
		t.Fatal(err)
	}
	wantDir, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	gotDir, wantDir = filepath.Clean(gotDir), filepath.Clean(wantDir)
	pathsMatch := gotDir == wantDir
	if runtime.GOOS == "windows" {
		pathsMatch = strings.EqualFold(gotDir, wantDir)
	}
	if !pathsMatch || filepath.Base(invoked.Path) != "codex" {
		t.Fatalf("launcher command = path %q, dir %q; want codex in %q", invoked.Path, invoked.Dir, workspace)
	}
	wantOutputPath := filepath.ToSlash(wantDir)
	if !strings.Contains(filepath.ToSlash(output.String()), "Opening codex worktree "+wantOutputPath) {
		t.Fatalf("open output does not identify selected workspace: %s", output.String())
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

func TestWorktreeSyncDryRunChecksMergeWithoutChangingBranches(t *testing.T) {
	root, source := integrationFixture(t, "sync preview")
	if err := os.WriteFile(filepath.Join(root, "target-only.txt"), []byte("target update\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "target-only.txt")
	runGitTest(t, root, "commit", "-m", "target update")
	targetBefore, _ := git(root, "rev-parse", "HEAD")
	sourceBefore, _ := git(source, "rev-parse", "HEAD")
	t.Chdir(root)
	var output bytes.Buffer
	if err := worktrees([]string{"sync", "feature", "--dry-run"}, &output); err != nil {
		t.Fatalf("sync dry-run failed: %v\n%s", err, output.String())
	}
	targetAfter, _ := git(root, "rev-parse", "HEAD")
	sourceAfter, _ := git(source, "rev-parse", "HEAD")
	if targetAfter != targetBefore || sourceAfter != sourceBefore {
		t.Fatal("sync dry-run changed an original branch")
	}
	if _, err := os.Stat(filepath.Join(source, "target-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("sync dry-run changed task worktree: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ai-init-sync-") {
			t.Fatalf("disposable preview remains: %s", entry.Name())
		}
	}
	if !strings.Contains(output.String(), "Sync preview passed") {
		t.Fatalf("preview result missing: %s", output.String())
	}
}

func TestWorktreeSyncDryRunConflictLeavesBothBranchesUntouched(t *testing.T) {
	root, source := integrationFixture(t, "sync conflict preview")
	if err := os.WriteFile(filepath.Join(root, "value.go"), []byte("package sample\n\nfunc Value() int { return 3 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "value.go")
	runGitTest(t, root, "commit", "-m", "target conflicting edit")
	if err := os.WriteFile(filepath.Join(source, "value.go"), []byte("package sample\n\nfunc Value() int { return 4 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, source, "add", "value.go")
	runGitTest(t, source, "commit", "-m", "task conflicting edit")
	targetBefore, _ := git(root, "rev-parse", "HEAD")
	sourceBefore, _ := git(source, "rev-parse", "HEAD")
	t.Chdir(root)
	var output bytes.Buffer
	if err := worktrees([]string{"sync", "feature", "--dry-run"}, &output); err == nil {
		t.Fatal("expected sync conflict")
	}
	targetAfter, _ := git(root, "rev-parse", "HEAD")
	sourceAfter, _ := git(source, "rev-parse", "HEAD")
	if targetAfter != targetBefore || sourceAfter != sourceBefore {
		t.Fatal("conflicting sync preview changed an original branch")
	}
	status, err := git(source, "status", "--porcelain")
	if err != nil || status != "" {
		t.Fatalf("conflicting preview dirtied task worktree: %q err=%v", status, err)
	}
	entries, err := os.ReadDir(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ai-init-sync-") {
			t.Fatalf("conflicting preview left disposable worktree: %s", entry.Name())
		}
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

func TestSetupGitattributesAddsManagedRulesToEmptyGitRepo(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root, "--with", "gitattributes"}, &output, &output); err != nil {
		t.Fatalf("setup: %v\n%s", err, output.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"* text=auto", "*.sh text eol=lf", "*.bat text eol=crlf", "*.cmd text eol=crlf", "*.ps1 text eol=crlf"} {
		if !strings.Contains(string(data), rule) {
			t.Fatalf("missing rule %q in %s", rule, data)
		}
	}
	attr, err := git(root, "check-attr", "text", "eol", "--", "windows-script.cmd")
	if err != nil || !strings.Contains(attr, "text: set") || !strings.Contains(attr, "eol: crlf") {
		t.Fatalf("Git did not apply the expected Windows script attributes: %q (%v)", attr, err)
	}
	status, err := git(root, "status", "--porcelain")
	if err != nil || strings.TrimSpace(status) != "?? .gitattributes" {
		t.Fatalf("unexpected worktree changes after setup: %q (%v)", status, err)
	}
	var again bytes.Buffer
	if err = Run([]string{"setup", "--path", root, "--with", "gitattributes"}, &again, &again); err != nil {
		t.Fatalf("repeat setup: %v\n%s", err, again.String())
	}
	dataAgain, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil || !bytes.Equal(data, dataAgain) {
		t.Fatalf("managed attributes are not idempotent: err=%v\n%s", err, dataAgain)
	}
}

func TestSetupGitattributesSkipsTrackedCRLFFiles(t *testing.T) {
	root, _ := integrationFixture(t, "tracked CRLF files")
	tracked := filepath.Join(root, "windows-script.cmd")
	contents := []byte("@echo off\r\necho safe\r\n")
	if err := os.WriteFile(tracked, contents, 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "windows-script.cmd")
	runGitTest(t, root, "commit", "-m", "add CRLF script")
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root, "--with", "gitattributes"}, &output, &output); err != nil {
		t.Fatalf("setup: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "tracked files exist") {
		t.Fatalf("tracked-file safety skip was not reported: %s", output.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".gitattributes")); !os.IsNotExist(err) {
		t.Fatalf("setup wrote .gitattributes into a tracked repository: %v", err)
	}
	after, err := os.ReadFile(tracked)
	if err != nil || !bytes.Equal(after, contents) {
		t.Fatalf("setup changed existing CRLF file bytes: %q (%v)", after, err)
	}
	status, err := git(root, "status", "--porcelain")
	if err != nil || status != "" {
		t.Fatalf("setup dirtied the tracked repository: %q (%v)", status, err)
	}
}

func TestSetupGitattributesPreservesExistingCustomRules(t *testing.T) {
	root := t.TempDir()
	existing := "*.bin binary\n*.txt text eol=lf\n"
	path := filepath.Join(root, ".gitattributes")
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root, "--with", "gitattributes"}, &output, &output); err != nil {
		t.Fatalf("setup: %v\n%s", err, output.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != existing || !strings.Contains(output.String(), "custom attributes detected") {
		t.Fatalf("custom attributes were not preserved/reported: %q\n%s", after, output.String())
	}
}

func TestSetupGitattributesDryRunAndMalformedMarkers(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root, "--with", "gitattributes", "--dry-run"}, &output, &output); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(output.String(), "+*.cmd text eol=crlf") {
		t.Fatalf("dry-run omitted the proposed attributes: %s", output.String())
	}
	path := filepath.Join(root, ".gitattributes")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote .gitattributes: %v", err)
	}
	if err := os.WriteFile(path, []byte("# ai-init gitattributes:begin\n*.bin binary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	if err := writeGitAttributes(root, false, false, &output); err == nil {
		t.Fatal("expected malformed managed block to be rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("malformed block was modified: %q err=%v", after, err)
	}
}

func TestSetupHooksInstallsAndExposesStagedChecks(t *testing.T) {
	putFakeAIInitOnPathCLI(t)
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	root, _ := integrationFixture(t, "pre-commit hooks")
	var output bytes.Buffer
	if err := Run([]string{"setup", "--path", root, "--with", "hooks"}, &output, &output); err != nil {
		t.Fatalf("setup hooks: %v\n%s", err, output.String())
	}
	hooksDir, err := git(root, "rev-parse", "--git-path", "hooks")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(root, hooksDir)
	}
	hook, err := os.ReadFile(filepath.Join(hooksDir, "pre-commit"))
	if err != nil || !strings.Contains(string(hook), "exec ai-init hooks check-staged") {
		t.Fatalf("setup did not install the expected hook: %q (%v)", hook, err)
	}
	output.Reset()
	if err = Run([]string{"hooks", "check-staged", "--path", root}, &output, &output); err != nil {
		t.Fatalf("staged check command: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Staged-content safeguards passed") {
		t.Fatalf("staged check result missing: %s", output.String())
	}
}

func putFakeAIInitOnPathCLI(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name := "ai-init"
	content := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		name = "ai-init.cmd"
		content = []byte("@echo off\r\nexit /b 0\r\n")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
