package hooks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const preCommit = "#!/bin/sh\n# ai-init managed pre-commit safeguard\nexec ai-init hooks check-staged\n"

var secretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY-----`)},
	{"AWS access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{30,}|github_pat_[A-Za-z0-9_]{50,})\b`)},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"Stripe live key", regexp.MustCompile(`\bsk_live_[0-9A-Za-z]{24,}\b`)},
	{"npm token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)},
	{"PyPI token", regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]{50,}\b`)},
}

// Install adds an ai-init pre-commit hook without replacing custom hook systems.
func Install(root string, dry bool, out io.Writer) error {
	return install(root, dry, out, exec.LookPath)
}

func install(root string, dry bool, out io.Writer, lookPath func(string) (string, error)) error {
	customPath, err := git(root, "config", "--get", "core.hooksPath")
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return fmt.Errorf("inspect core.hooksPath: %w", err)
		}
	}
	if customPath != "" {
		fmt.Fprintf(out, "  preserved custom Git hooks path %q (ai-init hook not installed; compose 'ai-init hooks check-staged' with your existing hook if desired)\n", customPath)
		return nil
	}
	hooksDir, err := git(root, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return fmt.Errorf("locate Git hooks directory: %w", err)
	}
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(root, hooksDir)
	}
	if info, statErr := os.Lstat(hooksDir); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(out, "  preserved Git hooks symlink %s (ai-init hook not installed)\n", hooksDir)
		return nil
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect Git hooks directory %s: %w", hooksDir, statErr)
	}
	path := filepath.Join(hooksDir, "pre-commit")
	if _, err = os.Lstat(path); err == nil {
		fmt.Fprintf(out, "  preserved existing pre-commit hook %s (compose 'ai-init hooks check-staged' with it if desired)\n", path)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect pre-commit hook %s: %w", path, err)
	}
	if _, err = lookPath("ai-init"); err != nil {
		fmt.Fprintln(out, "  skipped pre-commit hook (ai-init is not on PATH; add it and rerun setup --with hooks)")
		return nil
	}
	if dry {
		fmt.Fprintf(out, "  would install staged-content safeguards at %s\n", path)
		return nil
	}
	if err = os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create Git hooks directory %s: %w", hooksDir, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		if os.IsExist(err) {
			fmt.Fprintf(out, "  preserved concurrently created pre-commit hook %s\n", path)
			return nil
		}
		return fmt.Errorf("create pre-commit hook %s: %w", path, err)
	}
	if _, err = file.WriteString(preCommit); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write pre-commit hook %s: %w", path, err)
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("sync pre-commit hook %s: %w", path, err)
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close pre-commit hook %s: %w", path, err)
	}
	if err = os.Chmod(path, 0755); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("make pre-commit hook executable %s: %w", path, err)
	}
	fmt.Fprintf(out, "  installed staged-content safeguards at %s\n", path)
	return nil
}

// CheckStaged inspects only staged file names and added lines. It never reads the
// unstaged working-tree version of a file and never includes matched values in output.
func CheckStaged(root string, out io.Writer) error {
	names, err := gitBytes(root, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil {
		return fmt.Errorf("list staged files: %w", err)
	}
	var findings []string
	for _, name := range strings.Split(string(names), "\x00") {
		if name == "" {
			continue
		}
		normalized := strings.TrimPrefix(filepath.ToSlash(name), "./")
		base := filepath.Base(name)
		if base == ".agent-task.md" || base == ".ai-init.env.json" {
			findings = append(findings, fmt.Sprintf("%s: task-local ai-init metadata must not be committed", name))
			continue
		}
		patch, diffErr := gitBytes(root, "diff", "--cached", "--no-ext-diff", "--unified=0", "--", normalized)
		if diffErr != nil {
			return fmt.Errorf("read staged changes for %q: %w", name, diffErr)
		}
		for _, line := range bytes.Split(patch, []byte{'\n'}) {
			if !bytes.HasPrefix(line, []byte("+")) || bytes.HasPrefix(line, []byte("+++")) {
				continue
			}
			content := strings.TrimSuffix(strings.TrimPrefix(string(line), "+"), "\r")
			if strings.HasPrefix(content, "<<<<<<< ") || strings.HasPrefix(content, ">>>>>>> ") {
				findings = append(findings, fmt.Sprintf("%s: staged unresolved merge-conflict marker", name))
			}
			if strings.Contains(content, "AI_INIT_UNSAFE_PLACEHOLDER") {
				findings = append(findings, fmt.Sprintf("%s: staged ai-init unsafe placeholder marker", name))
			}
			for _, secret := range secretPatterns {
				if secret.pattern.MatchString(content) {
					findings = append(findings, fmt.Sprintf("%s: staged content resembles a %s", name, secret.name))
				}
			}
		}
	}
	if len(findings) > 0 {
		fmt.Fprintln(out, "ai-init pre-commit checks found staged content requiring review:")
		for _, finding := range findings {
			fmt.Fprintf(out, "  - %s\n", finding)
		}
		return errors.New("staged-content safeguards rejected the commit; remove or review the listed content and stage the intended changes")
	}
	fmt.Fprintln(out, "Staged-content safeguards passed")
	return nil
}

func git(root string, args ...string) (string, error) {
	data, err := gitBytes(root, args...)
	return strings.TrimSpace(string(data)), err
}

func gitBytes(root string, args ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	data, err := command.Output()
	if err != nil {
		return nil, err
	}
	return data, nil
}
