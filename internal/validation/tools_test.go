package validation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/naoufalelbani/init/internal/analyzer"
)

func TestCheckProjectDeduplicatesToolchainsAndReportsDetectedHints(t *testing.T) {
	var lookedUp []string
	project := analyzer.Project{
		Git:   true,
		Build: []string{"go build ./...", "npm run build"},
		Tests: []string{"go test ./...", "npm test"},
	}
	checks := CheckProject(t.TempDir(), project, "linux", func(command string) (string, error) {
		lookedUp = append(lookedUp, command)
		return "/tools/" + command, nil
	})
	if !reflect.DeepEqual(lookedUp, []string{"git", "go", "npm"}) {
		t.Fatalf("lookups=%v", lookedUp)
	}
	if len(checks) != 3 {
		t.Fatalf("checks=%+v", checks)
	}
	for _, check := range checks {
		if check.Err != nil || check.Path == "" {
			t.Errorf("unexpected unavailable tool: %+v", check)
		}
		if check.Command == "go" && len(check.Sources) != 2 {
			t.Errorf("Go usage sources=%v, want build and test", check.Sources)
		}
	}
}

func TestCheckProjectRequiresLocalBuildWrappers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mvnw.cmd"), []byte("@echo off\r\nexit /b 0\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	checks := CheckProject(root, analyzer.Project{Build: []string{"mvnw.cmd package"}}, "windows", func(string) (string, error) {
		t.Fatal("local wrapper should not be searched on PATH")
		return "", nil
	})
	if len(checks) != 1 || checks[0].Err != nil || checks[0].Path != filepath.Join(root, "mvnw.cmd") {
		t.Fatalf("checks=%+v", checks)
	}
	if err := os.Remove(filepath.Join(root, "mvnw.cmd")); err != nil {
		t.Fatal(err)
	}
	checks = CheckProject(root, analyzer.Project{Build: []string{"mvnw.cmd package"}}, "windows", func(string) (string, error) {
		t.Fatal("missing local wrapper must not fall back to PATH")
		return "", nil
	})
	if len(checks) != 1 || checks[0].Err == nil {
		t.Fatalf("missing wrapper was not detected: %+v", checks)
	}
}

func TestCheckProjectSkipsUnknownAndInspectionHints(t *testing.T) {
	checks := CheckProject(t.TempDir(), analyzer.Project{
		Build: []string{"unknown (pyproject.toml detected)", "inspect package.json scripts"},
	}, "linux", func(string) (string, error) {
		t.Fatal("no tool should be required")
		return "", nil
	})
	if len(checks) != 0 {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestCheckProjectAcceptsPythonExecutableFallback(t *testing.T) {
	checks := CheckProject(t.TempDir(), analyzer.Project{Languages: []string{"Python"}}, "linux", func(command string) (string, error) {
		if command == "python3" {
			return "", os.ErrNotExist
		}
		if command == "python" {
			return "/tools/python", nil
		}
		t.Fatalf("unexpected lookup %q", command)
		return "", nil
	})
	if len(checks) != 1 || checks[0].Err != nil || checks[0].Path != "/tools/python" {
		t.Fatalf("Python fallback check=%+v", checks)
	}
}

func TestCheckProjectChecksExplicitNodePackageManager(t *testing.T) {
	checks := CheckProject(t.TempDir(), analyzer.Project{
		DependencyManagers: []string{"pnpm (packageManager field)"},
	}, "linux", func(command string) (string, error) {
		if command != "pnpm" {
			t.Fatalf("unexpected lookup %q", command)
		}
		return "/tools/pnpm", nil
	})
	if len(checks) != 1 || checks[0].Command != "pnpm" || checks[0].Path != "/tools/pnpm" || checks[0].Err != nil {
		t.Fatalf("package manager check=%+v", checks)
	}
}
