package agents

import "fmt"

// LaunchCommand returns the documented interactive command for an adapter.
// The caller should set the process working directory to the target project.
func LaunchCommand(name string) (string, []string, error) {
	switch name {
	case "cursor":
		return "cursor", []string{".", "--classic"}, nil
	case "codex":
		return "codex", nil, nil
	case "claude":
		return "claude", nil, nil
	case "opencode":
		return "opencode", []string{"."}, nil
	case "aider":
		return "aider", nil, nil
	case "antigravity", "cline", "copilot", "windsurf":
		return "", nil, fmt.Errorf("launching %s is not supported by ai-init; open the worktree manually at the path printed by 'ai-init wt list'", name)
	default:
		return "", nil, fmt.Errorf("unknown agent adapter %q", name)
	}
}
