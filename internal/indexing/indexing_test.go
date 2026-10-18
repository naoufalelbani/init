package indexing

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestDiscoverUsesGitAndHonorsIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	write(t, root, ".gitignore", "ignored.go\n")
	write(t, root, "main.go", "package main\n")
	write(t, root, "ignored.go", "package ignored\n")
	write(t, root, filepath.Join("new dir", "naïve file.ts"), "export {}\n")
	runGit(t, root, "add", "--", ".gitignore", "main.go")

	inventory, err := Discover(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Source != "Git index and non-ignored files" {
		t.Fatalf("unexpected inventory source: %q", inventory.Source)
	}
	if !slices.Contains(inventory.Files, "main.go") || !slices.Contains(inventory.Files, "new dir/naïve file.ts") {
		t.Fatalf("tracked or non-ignored source file missing: %v", inventory.Files)
	}
	if slices.Contains(inventory.Files, "ignored.go") {
		t.Fatalf("Git-ignored source file leaked into inventory: %v", inventory.Files)
	}
}

func TestDiscoverFilesystemFallbackSkipsGeneratedAndSensitivePaths(t *testing.T) {
	root := t.TempDir()
	for path, contents := range map[string]string{
		filepath.Join("src", "app.py"):                   "print('ok')\n",
		filepath.Join("node_modules", "pkg", "index.js"): "module.exports = {}\n",
		filepath.Join("vendor", "pkg", "lib.go"):         "package pkg\n",
		filepath.Join(".ai", "generated", "map.go"):      "package generated\n",
		filepath.Join("config", "credentials.go"):        "package config\n",
		filepath.Join("ключи", "private_key_loader.rs"):  "fn main() {}\n",
	} {
		write(t, root, path, contents)
	}

	inventory, err := Discover(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Source != "filesystem scan" || inventory.Total != 1 || len(inventory.Files) != 1 || inventory.Files[0] != "src/app.py" {
		t.Fatalf("unexpected fallback inventory: %#v", inventory)
	}
}

func TestDiscoverBoundsAndSortsInventory(t *testing.T) {
	root := t.TempDir()
	for i := maxFiles; i >= 0; i-- {
		write(t, root, filepath.Join("src", string(rune('a'+i/26))+string(rune('a'+i%26))+".go"), "package sample\n")
	}
	inventory, err := Discover(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Total != maxFiles+1 || !inventory.Truncated || len(inventory.Files) != maxFiles {
		t.Fatalf("inventory was not bounded: total=%d truncated=%t listed=%d", inventory.Total, inventory.Truncated, len(inventory.Files))
	}
	if !slices.IsSorted(inventory.Files) {
		t.Fatalf("inventory is not sorted: %v", inventory.Files)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func write(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}
