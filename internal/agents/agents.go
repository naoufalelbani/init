package agents

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const begin = "<!-- ai-init adapter:begin -->"
const end = "<!-- ai-init adapter:end -->"

type Adapter struct {
	Name, Description string
	Paths             []string
	Detect            func(string) bool
	Render            func() string
}

var registry = []Adapter{
	{Name: "antigravity", Description: "Uses the native root AGENTS.md instructions (Google Antigravity Rules documentation).", Paths: []string{"AGENTS.md"}, Detect: func(root string) bool { return exists(root, ".agents", ".agent") }},
	{Name: "codex", Description: "Uses the canonical root AGENTS.md.", Paths: []string{"AGENTS.md"}, Detect: func(string) bool { return false }},
	{Name: "claude", Description: "Imports canonical instructions using Claude Code's documented @path syntax.", Paths: []string{"CLAUDE.md"}, Detect: func(root string) bool { return exists(root, ".claude") }, Render: func() string { return begin + "\n@AGENTS.md\n" + end + "\n" }},
	{Name: "cursor", Description: "Creates an always-applied Cursor MDC project rule that includes AGENTS.md.", Paths: []string{".cursor/rules/ai-init.mdc"}, Detect: func(root string) bool { return exists(root, ".cursor") }, Render: func() string {
		return "---\ndescription: Follow the repository's canonical AI development instructions\nglobs:\nalwaysApply: true\n---\n\n" + begin + "\n@AGENTS.md\n" + end + "\n"
	}},
	{Name: "opencode", Description: "Uses the canonical root AGENTS.md, supported natively by OpenCode.", Paths: []string{"AGENTS.md"}, Detect: func(root string) bool { return exists(root, "opencode.json", "opencode.jsonc", ".opencode") }},
	{Name: "cline", Description: "Uses the canonical AGENTS.md, supported natively by Cline.", Paths: []string{"AGENTS.md"}, Detect: func(root string) bool { return exists(root, ".clinerules", ".cline/rules") }},
	{Name: "copilot", Description: "Creates GitHub Copilot repository instructions that direct agents to the canonical AGENTS.md.", Paths: []string{".github/copilot-instructions.md"}, Detect: func(root string) bool { return exists(root, ".github/copilot-instructions.md", ".github/instructions") }, Render: func() string {
		return begin + "\nRead and follow the repository's canonical development contract in `AGENTS.md`.\n" + end + "\n"
	}},
	{Name: "aider", Description: "Creates Aider's documented read-only config entry for AGENTS.md when no project config exists.", Paths: []string{".aider.conf.yml"}, Detect: func(root string) bool { return exists(root, ".aider.conf.yml") }, Render: func() string { return "# ai-init managed instructions\nread: [AGENTS.md]\n" }},
	{Name: "windsurf", Description: "Creates an always-on Windsurf rule that references AGENTS.md.", Paths: []string{".windsurf/rules/ai-init.md"}, Detect: func(root string) bool { return exists(root, ".windsurf", ".windsurfrules") }, Render: func() string {
		return "---\ntrigger: always_on\n---\n\n" + begin + "\nFollow the repository's canonical development contract in `AGENTS.md`.\n" + end + "\n"
	}},
}

func List() []Adapter { out := make([]Adapter, len(registry)); copy(out, registry); return out }
func Names() []string {
	out := make([]string, 0, len(registry))
	for _, a := range registry {
		out = append(out, a.Name)
	}
	return out
}
func exists(root string, paths ...string) bool {
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			return true
		}
	}
	return false
}

// Configure generates only documented adapters. An empty selection configures detected adapters.
func Configure(root string, selection []string, dry bool, out io.Writer) error {
	selected := map[string]bool{}
	for _, name := range selection {
		selected[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for name := range selected {
		found := false
		for _, adapter := range registry {
			if name == adapter.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown agent adapter %q", name)
		}
	}
	explicit := len(selection) > 0
	for _, a := range registry {
		use := selected[a.Name]
		if !explicit && a.Detect != nil {
			use = a.Detect(root)
		}
		if a.Name == "codex" && explicit && selected[a.Name] {
			use = true
		}
		if !use {
			continue
		}
		if a.Render == nil {
			fmt.Fprintf(out, "  %s: supported natively; canonical AGENTS.md is authoritative\n", a.Name)
			continue
		}
		path := filepath.Join(root, a.Paths[0])
		if existing, statErr := os.ReadFile(path); statErr == nil && !strings.Contains(string(existing), begin) {
			fmt.Fprintf(out, "  %s: preserved existing custom configuration\n", a.Name)
			continue
		}
		content := a.Render()
		if err := write(path, content, dry); err != nil {
			return fmt.Errorf("configure %s: %w", a.Name, err)
		}
		if dry {
			fmt.Fprintf(out, "  %s: would configure %s\n", a.Name, path)
		} else {
			fmt.Fprintf(out, "  %s: configured %s\n", a.Name, path)
		}
	}
	return nil
}
func write(path, content string, dry bool) error {
	old, err := os.ReadFile(path)
	if err == nil {
		if bytes.Equal(old, []byte(content)) {
			return nil
		}
		if strings.Contains(string(old), begin) && strings.Contains(string(old), end) {
			text := string(old)
			i := strings.Index(text, begin)
			relative := strings.Index(text[i:], end)
			if relative < 0 {
				return fmt.Errorf("managed block is incomplete; preserving %s", path)
			}
			j := i + relative + len(end)
			newBegin := strings.Index(content, begin)
			newEndRelative := -1
			if newBegin >= 0 {
				newEndRelative = strings.Index(content[newBegin:], end)
			}
			if newBegin < 0 || newEndRelative < 0 {
				return fmt.Errorf("adapter renderer omitted managed markers for %s", path)
			}
			newEnd := newBegin + newEndRelative + len(end)
			content = text[:i] + content[newBegin:newEnd] + text[j:]
			if bytes.Equal(old, []byte(content)) {
				return nil
			}
			if dry {
				return nil
			}
			backup := path + ".ai-init.bak"
			if _, e := os.Stat(backup); os.IsNotExist(e) {
				if e = os.WriteFile(backup, old, 0600); e != nil {
					return e
				}
			}
			return replace(path, []byte(content))
		}
		return nil // Existing unmarked configuration may contain user rules; never replace it.
	}
	if !os.IsNotExist(err) {
		return err
	}
	if dry {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
func replace(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	displaced := path + ".replace"
	if err := os.Rename(path, displaced); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Rename(displaced, path)
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(displaced)
	return nil
}
