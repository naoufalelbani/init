package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
