package indexing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	serenaIndexTimeout   = 3 * time.Minute
	graphifyQueryTimeout = 30 * time.Second
	maxIndexerOutput     = 1 << 20
)

var (
	hubLabelPattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:/() -]{0,79}$`)
	sensitiveHubPattern = regexp.MustCompile(`(?i)(password|passwd|secret|credential|token|api[_ -]?key|private[_ -]?key|access[_ -]?key|database[_ -]?url|dsn)`)
	longOpaqueLabel     = regexp.MustCompile(`^[A-Za-z0-9_-]{32,}$`)
)

// IndexerContext contains a small, safe-to-persist summary retrieved from
// locally installed project indexers. It deliberately excludes raw source or
// tool output.
type IndexerContext struct {
	SerenaStatus   string
	GraphifyStatus string
	GraphifyHubs   []string
}

type LookupFunc func(string) (string, error)
type CommandFunc func(context.Context, string, string, []string, []string) ([]byte, error)

// UseInstalled refreshes an existing Serena symbol index and reads the top
// hubs from a conventional local Graphify graph. It never installs tools or
// executes arbitrary MCP configuration. Missing or failing tools are reported
// in the returned context so callers can continue with the deterministic scan.
func UseInstalled(root string, lookPath LookupFunc, run CommandFunc) IndexerContext {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if run == nil {
		run = runIndexerCommand
	}
	result := IndexerContext{SerenaStatus: "not configured", GraphifyStatus: "not configured"}

	serenaConfig := filepath.Join(root, ".serena", "project.yml")
	if info, err := os.Stat(serenaConfig); err == nil && info.Mode().IsRegular() {
		result.SerenaStatus = "configured; not refreshed"
		if executable, lookupErr := lookPath("serena"); lookupErr != nil {
			result.SerenaStatus = "configured; Serena CLI unavailable"
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), serenaIndexTimeout)
			_, indexErr := run(ctx, root, executable, []string{"project", "index", root}, safeIndexerEnvironment(nil))
			cancel()
			if indexErr != nil {
				result.SerenaStatus = "index refresh failed; deterministic fallback used"
			} else {
				result.SerenaStatus = "symbol index refreshed"
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		result.SerenaStatus = "configuration could not be inspected; deterministic fallback used"
	}

	graph := filepath.Join(root, "graphify-out", "graph.json")
	if info, err := os.Stat(graph); err == nil && info.Mode().IsRegular() {
		result.GraphifyStatus = "graph present; not queried"
		if executable, lookupErr := lookPath("graphify"); lookupErr != nil {
			result.GraphifyStatus = "graph present; Graphify CLI unavailable"
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), graphifyQueryTimeout)
			output, queryErr := run(ctx, root, executable, []string{"god-nodes", "--json", "--graph", graph}, safeIndexerEnvironment([]string{"GRAPHIFY_NO_AUTO_REFRESH=1"}))
			cancel()
			if queryErr != nil {
				result.GraphifyStatus = "graph query failed; deterministic fallback used"
			} else {
				result.GraphifyHubs = safeGraphifyHubs(output)
				if len(result.GraphifyHubs) == 0 {
					result.GraphifyStatus = "graph queried; no safe hubs to include"
				} else {
					result.GraphifyStatus = fmt.Sprintf("existing graph queried; %d hubs included", len(result.GraphifyHubs))
				}
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		result.GraphifyStatus = "graph could not be inspected; deterministic fallback used"
	}
	return result
}

func safeGraphifyHubs(output []byte) []string {
	if len(output) == 0 || len(output) > maxIndexerOutput {
		return nil
	}
	var nodes []struct {
		Label  string `json:"label"`
		Degree int    `json:"degree"`
	}
	if err := json.Unmarshal(output, &nodes); err != nil {
		return nil
	}
	var hubs []string
	seen := make(map[string]bool)
	for _, node := range nodes {
		label := strings.TrimSpace(node.Label)
		if node.Degree < 1 || !hubLabelPattern.MatchString(label) || sensitiveHubPattern.MatchString(label) || longOpaqueLabel.MatchString(label) || seen[label] {
			continue
		}
		seen[label] = true
		hubs = append(hubs, fmt.Sprintf("%s (degree %d)", label, node.Degree))
		if len(hubs) == 8 {
			break
		}
	}
	return hubs
}

func safeIndexerEnvironment(extra []string) []string {
	allowed := map[string]bool{
		"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true,
		"HOME": true, "USERPROFILE": true, "HOMEDRIVE": true, "HOMEPATH": true,
		"APPDATA": true, "LOCALAPPDATA": true, "XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true,
		"TEMP": true, "TMP": true, "TMPDIR": true, "LANG": true, "LC_ALL": true,
		"JAVA_HOME": true, "GOROOT": true, "SERENA_CONFIG": true,
	}
	env := make([]string, 0, len(allowed)+len(extra))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && allowed[strings.ToUpper(key)] {
			env = append(env, entry)
		}
	}
	for _, entry := range extra {
		env = append(env, entry)
	}
	return env
}

func runIndexerCommand(ctx context.Context, directory, executable string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = directory
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("indexer output could not be read")
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, errors.New("indexer command failed")
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maxIndexerOutput+1))
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("indexer output could not be read")
	}
	if len(output) > maxIndexerOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("indexer output exceeded the configured limit")
	}
	if err := cmd.Wait(); err != nil {
		return nil, errors.New("indexer command failed")
	}
	return output, nil
}

// IsSafeSummary verifies that persisted Graphify labels remain bounded,
// single-line structural names. It is exported for consumers that want to
// independently validate generated context before writing.
func IsSafeSummary(value string) bool {
	return hubLabelPattern.MatchString(value) && !sensitiveHubPattern.MatchString(value) && !longOpaqueLabel.MatchString(value) && !strings.ContainsAny(value, "\r\n")
}
