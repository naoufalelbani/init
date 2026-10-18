package indexing

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUseInstalledQueriesOnlyExistingConfiguredIndexes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".serena"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".serena", "project.yml"), []byte("language: go\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "graphify-out"), 0700); err != nil {
		t.Fatal(err)
	}
	graph := filepath.Join(root, "graphify-out", "graph.json")
	if err := os.WriteFile(graph, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	look := func(name string) (string, error) { return "/tools/" + name, nil }
	run := func(ctx context.Context, directory, executable string, args, env []string) ([]byte, error) {
		calls = append(calls, append([]string{executable}, args...))
		if directory != root {
			t.Fatalf("directory=%q", directory)
		}
		for _, entry := range env {
			if strings.HasPrefix(strings.ToUpper(entry), "AI_INIT_TEST_SECRET=") {
				t.Fatal("secret environment variable reached indexer")
			}
		}
		if executable == "/tools/serena" {
			for _, entry := range env {
				if entry == "GRAPHIFY_NO_AUTO_REFRESH=1" {
					t.Fatal("Graphify-only refresh guard reached Serena")
				}
			}
			return nil, nil
		}
		if !containsEnv(env, "GRAPHIFY_NO_AUTO_REFRESH=1") {
			t.Fatal("Graphify auto-refresh guard missing")
		}
		return []byte(`[{"id":"safe","label":"internal/indexing","degree":12},{"id":"secret","label":"database_password","degree":99},{"id":"opaque","label":"abcdefghijklmnopqrstuvwxyz0123456789","degree":50}]`), nil
	}
	t.Setenv("AI_INIT_TEST_SECRET", "never-forward")
	got := UseInstalled(root, look, run)
	if got.SerenaStatus != "symbol index refreshed" {
		t.Fatalf("Serena status=%q", got.SerenaStatus)
	}
	if got.GraphifyStatus != "existing graph queried; 1 hubs included" {
		t.Fatalf("Graphify status=%q", got.GraphifyStatus)
	}
	if want := []string{"internal/indexing (degree 12)"}; !reflect.DeepEqual(got.GraphifyHubs, want) {
		t.Fatalf("hubs=%v want=%v", got.GraphifyHubs, want)
	}
	if len(calls) != 2 || !reflect.DeepEqual(calls[0], []string{"/tools/serena", "project", "index", root}) || !reflect.DeepEqual(calls[1], []string{"/tools/graphify", "god-nodes", "--json", "--graph", graph}) {
		t.Fatalf("unexpected commands: %#v", calls)
	}
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func TestUseInstalledFallsBackWhenToolsAreUnavailable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".serena"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".serena", "project.yml"), []byte("language: go\n"), 0600); err != nil {
		t.Fatal(err)
	}
	graphDir := filepath.Join(root, "graphify-out")
	if err := os.MkdirAll(graphDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(graphDir, "graph.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	got := UseInstalled(root, func(string) (string, error) { return "", os.ErrNotExist }, nil)
	if got.SerenaStatus != "configured; Serena CLI unavailable" || got.GraphifyStatus != "graph present; Graphify CLI unavailable" {
		t.Fatalf("unexpected fallback statuses: %+v", got)
	}
	if len(got.GraphifyHubs) != 0 {
		t.Fatalf("unexpected hubs: %v", got.GraphifyHubs)
	}
}

func TestSafeIndexerEnvironmentAddsOnlyExplicitOverrides(t *testing.T) {
	t.Setenv("AI_INIT_TEST_SECRET", "secret-value")
	t.Setenv("AI_INIT_TEST_SAFE", "also-not-allow-listed")
	env := safeIndexerEnvironment([]string{"GRAPHIFY_NO_AUTO_REFRESH=1"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "AI_INIT_TEST_SECRET") || strings.Contains(joined, "AI_INIT_TEST_SAFE") {
		t.Fatalf("unexpected environment entries: %s", joined)
	}
	if !strings.Contains(joined, "GRAPHIFY_NO_AUTO_REFRESH=1") {
		t.Fatalf("Graphify refresh guard missing: %s", joined)
	}
}

func TestSafeGraphifyHubsRejectsUnsafeAndExcessLabels(t *testing.T) {
	input := `[{"label":"one","degree":1},{"label":"two","degree":2},{"label":"three","degree":3},{"label":"four","degree":4},{"label":"five","degree":5},{"label":"six","degree":6},{"label":"seven","degree":7},{"label":"eight","degree":8},{"label":"nine","degree":9},{"label":"contains\nnewline","degree":99}]`
	got := safeGraphifyHubs([]byte(input))
	if len(got) != 8 || got[0] != "one (degree 1)" || got[7] != "eight (degree 8)" {
		t.Fatalf("unexpected bounded hubs: %v", got)
	}
	if IsSafeSummary("safe\nsecond line") || IsSafeSummary("API_TOKEN") || !IsSafeSummary("internal/indexing") {
		t.Fatal("summary safety validation is incorrect")
	}
}
