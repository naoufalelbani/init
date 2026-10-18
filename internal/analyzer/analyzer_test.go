package analyzer

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAnalyzeGoProjectFacts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/project\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "server"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "server", "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != filepath.Base(root) || !slices.Contains(project.Languages, "Go") {
		t.Fatalf("project identity/language not detected: %#v", project)
	}
	if !slices.Contains(project.Build, "go build ./...") || !slices.Contains(project.Tests, "go test ./...") {
		t.Fatalf("Go commands not detected: %#v", project)
	}
	if !slices.Contains(project.Sources, "cmd") || !slices.Contains(project.EntryPoints, "cmd/server/main.go") {
		t.Fatalf("source or entry point not detected: %#v", project)
	}
}

func TestAnalyzeTypeScriptReactPackageManagerCommands(t *testing.T) {
	root := t.TempDir()
	manifest := `{"packageManager":"pnpm@10.0.0","scripts":{"build":"vite build","test":"vitest"},"dependencies":{"react":"^19.0.0","vite":"^6.0.0"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"JavaScript", "TypeScript"} {
		if !slices.Contains(project.Languages, language) {
			t.Fatalf("missing language %q: %#v", language, project.Languages)
		}
	}
	for _, framework := range []string{"Node.js", "react", "vite"} {
		if !slices.Contains(project.Frameworks, framework) {
			t.Fatalf("missing framework %q: %#v", framework, project.Frameworks)
		}
	}
	if !slices.Contains(project.Build, "pnpm run build") || !slices.Contains(project.Tests, "pnpm test") {
		t.Fatalf("package manager commands not detected: build=%v tests=%v", project.Build, project.Tests)
	}
	if !slices.Contains(project.DependencyManagers, "pnpm") || !slices.Contains(project.DependencyManagers, "pnpm (packageManager field)") {
		t.Fatalf("dependency manager not detected: %v", project.DependencyManagers)
	}
}

func TestAnalyzeExistingProjectConfigurationEvidence(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		".prettierrc.json", ".gitignore", "AGENTS.md", "Dockerfile", ".serena/project.yml",
		".cursor/rules/project.mdc", ".github/copilot-instructions.md", ".github/workflows/ci.yml",
	} {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("evidence\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	project, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	for field, values := range map[string][]string{
		"configuration":         project.Configurations,
		"agent instructions":    project.AgentInstructions,
		"indexer":               project.Indexers,
		"Git conventions":       project.GitConventions,
		"platform requirements": project.PlatformRequirements,
	} {
		if len(values) == 0 {
			t.Errorf("%s evidence was not detected", field)
		}
	}
	for _, path := range []string{".cursor/rules/project.mdc", ".github/copilot-instructions.md"} {
		if !slices.Contains(project.AgentInstructions, path) {
			t.Errorf("agent instruction path %q not detected: %v", path, project.AgentInstructions)
		}
	}
	if !slices.Contains(project.PlatformRequirements, ".github/workflows") {
		t.Errorf("CI workflow directory not detected: %v", project.PlatformRequirements)
	}
}

func TestAnalyzeDoesNotReclassifyManagedAdapterOutputAsCustomRules(t *testing.T) {
	root := t.TempDir()
	managed := "<!-- ai-init adapter:begin -->\n@AGENTS.md\n<!-- ai-init adapter:end -->\n"
	for _, path := range []string{"AGENTS.md", "CLAUDE.md", ".cursor/rules/ai-init.mdc", ".github/copilot-instructions.md"} {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
			t.Fatal(err)
		}
		body := managed
		if path == "AGENTS.md" {
			body = "<!-- ai-init:begin -->\nmanaged facts\n<!-- ai-init:end -->\n"
		}
		if err := os.WriteFile(fullPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	project, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.AgentInstructions) != 0 {
		t.Fatalf("managed ai-init output was reclassified as custom rules: %v", project.AgentInstructions)
	}
}

func TestAnalyzeEmptyDirectoryDoesNotInventStack(t *testing.T) {
	root := t.TempDir()
	project, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if project.Git || len(project.Languages) != 0 || len(project.Frameworks) != 0 || len(project.Build) != 0 || len(project.Tests) != 0 {
		t.Fatalf("empty directory produced speculative project facts: %#v", project)
	}
}

func TestAnalyzeReturnsErrorsForUnreadableInputs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(root); err == nil {
		t.Fatal("expected malformed package.json error")
	}
	if _, err := Analyze(filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected missing project directory error")
	}
}
