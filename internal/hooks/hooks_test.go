package hooks

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallAddsExecutableHookToEmptyGitHooksPath(t *testing.T) {
	isolateGitHooksConfig(t)
	putFakeAIInitOnPath(t)
	root := t.TempDir()
	runGitTest(t, root, "init")
	var output bytes.Buffer
	if err := Install(root, false, &output); err != nil {
		t.Fatalf("install: %v\n%s", err, output.String())
	}
	hooksDir := runGitTest(t, root, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(root, hooksDir)
	}
	path := filepath.Join(hooksDir, "pre-commit")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != preCommit {
		t.Fatalf("unexpected hook content: %q", data)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm()&0111 == 0 {
			t.Fatalf("hook is not executable: info=%v err=%v", info, statErr)
		}
	}
}

func TestInstallPreservesExistingHookAndCustomHooksPath(t *testing.T) {
	isolateGitHooksConfig(t)
	t.Run("existing hook", func(t *testing.T) {
		root := t.TempDir()
		runGitTest(t, root, "init")
		hooksDir := runGitTest(t, root, "rev-parse", "--git-path", "hooks")
		if !filepath.IsAbs(hooksDir) {
			hooksDir = filepath.Join(root, hooksDir)
		}
		if err := os.MkdirAll(hooksDir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(hooksDir, "pre-commit")
		custom := []byte("#!/bin/sh\necho user's hook\n")
		if err := os.WriteFile(path, custom, 0600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := Install(root, false, &output); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if !bytes.Equal(data, custom) || !strings.Contains(output.String(), "preserved existing pre-commit") {
			t.Fatalf("existing hook changed or was not reported: %q\n%s", data, output.String())
		}
	})
	t.Run("configured hooks path", func(t *testing.T) {
		root := t.TempDir()
		runGitTest(t, root, "init")
		runGitTest(t, root, "config", "--local", "core.hooksPath", "custom hooks")
		var output bytes.Buffer
		if err := Install(root, false, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "preserved custom Git hooks path") {
			t.Fatalf("custom hooks path was not preserved/reported: %s", output.String())
		}
		if _, err := os.Stat(filepath.Join(root, "custom hooks", "pre-commit")); !os.IsNotExist(err) {
			t.Fatalf("ai-init wrote into custom hooks path: %v", err)
		}
	})
	t.Run("global configured hooks path", func(t *testing.T) {
		root := t.TempDir()
		runGitTest(t, root, "init")
		globalConfig := filepath.Join(t.TempDir(), "gitconfig")
		if err := os.WriteFile(globalConfig, []byte("[core]\n\thooksPath = global-hooks\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
		t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
		var output bytes.Buffer
		if err := Install(root, false, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "preserved custom Git hooks path") {
			t.Fatalf("global custom hooks path was not preserved/reported: %s", output.String())
		}
	})
}

func TestInstallDryRunDoesNotCreateHook(t *testing.T) {
	isolateGitHooksConfig(t)
	putFakeAIInitOnPath(t)
	root := t.TempDir()
	runGitTest(t, root, "init")
	var output bytes.Buffer
	if err := Install(root, true, &output); err != nil {
		t.Fatal(err)
	}
	hooksDir := runGitTest(t, root, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(root, hooksDir)
	}
	if _, err := os.Stat(filepath.Join(hooksDir, "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created a hook: %v", err)
	}
	if !strings.Contains(output.String(), "would install staged-content safeguards") {
		t.Fatalf("dry-run report missing: %s", output.String())
	}
}

func TestInstallSkipsWhenAIBinaryIsNotOnPath(t *testing.T) {
	isolateGitHooksConfig(t)
	root := t.TempDir()
	runGitTest(t, root, "init")
	var output bytes.Buffer
	if err := install(root, false, &output, func(string) (string, error) {
		return "", exec.ErrNotFound
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ai-init is not on PATH") {
		t.Fatalf("missing PATH limitation report: %s", output.String())
	}
	hooksDir := runGitTest(t, root, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(root, hooksDir)
	}
	if _, err := os.Stat(filepath.Join(hooksDir, "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("installed a hook without its required executable: %v", err)
	}
}

func TestCheckStagedScansIndexOnlyAndDoesNotPrintSecret(t *testing.T) {
	root := testRepository(t)
	path := filepath.Join(root, "settings café.txt")
	secret := "AKIAABCDEFGHIJKLMNOP"
	if err := os.WriteFile(path, []byte("access="+secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "settings café.txt")
	if err := os.WriteFile(path, []byte("access=example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := CheckStaged(root, &output)
	if err == nil || !strings.Contains(output.String(), "AWS access key") {
		t.Fatalf("expected staged AWS key to be rejected: err=%v output=%s", err, output.String())
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("secret value leaked to hook output")
	}
	if !strings.Contains(output.String(), "settings café.txt") {
		t.Fatalf("finding omitted the staged path: %s", output.String())
	}
}

func TestCheckStagedFindsConflictPlaceholderAndTaskFilesButAllowsOrdinaryExamples(t *testing.T) {
	root := testRepository(t)
	safe := filepath.Join(root, "guide.md")
	if err := os.WriteFile(safe, []byte("Example: api_key = 'replace-me'\nA row of equals signs: =======\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "guide.md")
	var output bytes.Buffer
	if err := CheckStaged(root, &output); err != nil {
		t.Fatalf("ordinary documentation example rejected: %v\n%s", err, output.String())
	}
	conflict := filepath.Join(root, "conflict.txt")
	if err := os.WriteFile(conflict, []byte("<<<<<<< HEAD\nresolved later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "conflict.txt")
	if err := CheckStaged(root, &output); err == nil || !strings.Contains(output.String(), "unresolved merge-conflict marker") {
		t.Fatalf("unresolved marker was not rejected: err=%v output=%s", err, output.String())
	}
	placeholder := filepath.Join(root, "stub.go")
	if err := os.WriteFile(placeholder, []byte("// AI_INIT_UNSAFE_PLACEHOLDER\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "stub.go")
	if err := CheckStaged(root, &output); err == nil || !strings.Contains(output.String(), "unsafe placeholder") {
		t.Fatalf("known unsafe placeholder was not rejected: err=%v output=%s", err, output.String())
	}
	taskFile := filepath.Join(root, ".agent-task.md")
	if err := os.WriteFile(taskFile, []byte("task-local primer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "-f", ".agent-task.md")
	if err := CheckStaged(root, &output); err == nil || !strings.Contains(output.String(), "task-local ai-init metadata") {
		t.Fatalf("staged task metadata was not rejected: err=%v output=%s", err, output.String())
	}
	envFile := filepath.Join(root, ".ai-init.env.json")
	if err := os.WriteFile(envFile, []byte(`{"TOKEN":"value"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "-f", ".ai-init.env.json")
	if err := CheckStaged(root, &output); err == nil || !strings.Contains(output.String(), "task-local ai-init metadata") {
		t.Fatalf("staged task environment metadata was not rejected: err=%v output=%s", err, output.String())
	}
}

func testRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitTest(t, root, "init", "-b", "main")
	runGitTest(t, root, "config", "user.name", "ai-init hooks test")
	runGitTest(t, root, "config", "user.email", "ai-init-hooks@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "README.md")
	runGitTest(t, root, "commit", "-m", "base")
	return root
}

func putFakeAIInitOnPath(t *testing.T) {
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

func isolateGitHooksConfig(t *testing.T) {
	t.Helper()
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, data)
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
}
