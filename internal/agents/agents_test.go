package agents

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureCursorCreatesDocumentedMDCReference(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("canonical"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Configure(root, []string{"cursor"}, false, &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".cursor", "rules", "ai-init.mdc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"alwaysApply: true", "@AGENTS.md"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("rule missing %q: %s", want, data)
		}
	}
}
func TestConfigurePreservesExistingCustomAdapterFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "CLAUDE.md")
	want := "# my custom Claude instructions\n"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Configure(root, nil, false, &out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("custom config overwritten: %s", got)
	}
	if !strings.Contains(out.String(), "preserved existing custom configuration") {
		t.Fatalf("status not clear: %s", out.String())
	}
}
func TestConfigureRejectsUnknownAdapterBeforeWriting(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	if err := Configure(root, []string{"claude", "not-a-real-agent"}, false, &out); err == nil {
		t.Fatal("expected unknown adapter error")
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("configured files before validation: %v", err)
	}
}
func TestConfigureDryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	if err := Configure(root, []string{"claude"}, true, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote file: %v", err)
	}
}

func TestConfigureRejectsIncompleteManagedBlockWithoutChangingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".aider.conf.yml")
	want := "# " + begin + "\ncustom truncated content\n"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Configure(root, []string{"aider"}, false, &out); err == nil {
		t.Fatal("expected incomplete managed block error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("malformed custom file changed: %q", got)
	}
}

func TestConfigureCursorRefreshPreservesFilePreambleAndCustomTail(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".cursor", "rules", "ai-init.mdc")
	var out bytes.Buffer
	if err := Configure(root, []string{"cursor"}, false, &out); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	amended := strings.Replace(string(original), "description: Follow", "description: Custom", 1) + "\n# local note\n"
	if err = os.WriteFile(path, []byte(amended), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Configure(root, []string{"cursor"}, false, &out); err != nil {
		t.Fatal(err)
	}
	refreshed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(refreshed), "---") != 2 {
		t.Fatalf("duplicate MDC frontmatter: %s", refreshed)
	}
	if !strings.Contains(string(refreshed), "# local note") {
		t.Fatalf("custom tail lost: %s", refreshed)
	}
	if !strings.Contains(string(refreshed), "description: Custom") {
		t.Fatalf("custom preamble lost: %s", refreshed)
	}
}
