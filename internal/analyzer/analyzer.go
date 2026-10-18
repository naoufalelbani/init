package analyzer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Project struct {
	Name                                                      string
	Languages, Frameworks, Build, Tests, Sources, EntryPoints []string
	DependencyManagers, Configurations, AgentInstructions     []string
	Indexers, GitConventions, PlatformRequirements            []string
	Git                                                       bool
}

func Analyze(root string) (Project, error) {
	p := Project{Name: filepath.Base(root)}
	if _, e := os.Stat(filepath.Join(root, ".git")); e == nil {
		p.Git = true
	} else if !os.IsNotExist(e) {
		return Project{}, fmt.Errorf("inspect Git metadata: %w", e)
	}
	checks := map[string]string{"go.mod": "Go", "Cargo.toml": "Rust", "pubspec.yaml": "Dart", "pyproject.toml": "Python", "requirements.txt": "Python", "package.json": "JavaScript", "pom.xml": "Java", "build.gradle": "Java", "build.gradle.kts": "Kotlin / Java", "*.csproj": "C# / .NET", "CMakeLists.txt": "C / C++"}
	entries, err := os.ReadDir(root)
	if err != nil {
		return Project{}, fmt.Errorf("read project directory: %w", err)
	}
	names := map[string]bool{}
	for _, x := range entries {
		names[x.Name()] = true
	}
	if err := collectRepositoryMetadata(root, names, &p); err != nil {
		return Project{}, err
	}
	for f, l := range checks {
		ok := names[f]
		if strings.HasPrefix(f, "*.") {
			for n := range names {
				if strings.HasSuffix(n, strings.TrimPrefix(f, "*")) {
					ok = true
				}
			}
		}
		if ok {
			p.Languages = append(p.Languages, l)
		}
	}
	if names["package.json"] {
		b, err := os.ReadFile(filepath.Join(root, "package.json"))
		if err != nil {
			return Project{}, fmt.Errorf("read package.json: %w", err)
		}
		var manifest struct {
			Scripts         map[string]string `json:"scripts"`
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
			PackageManager  string            `json:"packageManager"`
		}
		if err := json.Unmarshal(b, &manifest); err != nil {
			return Project{}, fmt.Errorf("parse package.json: %w", err)
		}
		dependencies := map[string]bool{}
		for name := range manifest.Dependencies {
			dependencies[name] = true
		}
		for name := range manifest.DevDependencies {
			dependencies[name] = true
		}
		for _, name := range []string{"react", "vue", "vite", "next", "express", "@angular/core", "svelte", "@sveltejs/kit", "electron", "nuxt", "gatsby"} {
			if dependencies[name] {
				p.Frameworks = append(p.Frameworks, name)
			}
		}
		p.Frameworks = append(p.Frameworks, "Node.js")
		manager := "npm"
		declaredManager := strings.SplitN(manifest.PackageManager, "@", 2)[0]
		if declaredManager == "npm" || declaredManager == "pnpm" || declaredManager == "yarn" || declaredManager == "bun" {
			manager = declaredManager
		} else if names["pnpm-lock.yaml"] {
			manager = "pnpm"
		} else if names["yarn.lock"] {
			manager = "yarn"
		} else if names["bun.lockb"] || names["bun.lock"] {
			manager = "bun"
		}
		if declaredManager != "" {
			if declaredManager == "npm" || declaredManager == "pnpm" || declaredManager == "yarn" || declaredManager == "bun" {
				p.DependencyManagers = append(p.DependencyManagers, declaredManager+" (packageManager field)")
			} else {
				p.DependencyManagers = append(p.DependencyManagers, "unrecognized packageManager declaration")
			}
		}
		for _, lockfile := range []string{"package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock"} {
			if names[lockfile] {
				p.DependencyManagers = append(p.DependencyManagers, manager)
				break
			}
		}
		if names["tsconfig.json"] {
			p.Languages = append(p.Languages, "TypeScript")
		}
		if len(manifest.Scripts) > 0 {
			p.Build = append(p.Build, "inspect package.json scripts")
			if _, ok := manifest.Scripts["build"]; ok {
				if manager == "npm" {
					p.Build = append(p.Build, "npm run build")
				} else {
					p.Build = append(p.Build, manager+" run build")
				}
			}
			if _, ok := manifest.Scripts["test"]; ok {
				if manager == "pnpm" {
					p.Tests = append(p.Tests, "pnpm test")
				} else if manager == "yarn" {
					p.Tests = append(p.Tests, "yarn test")
				} else if manager == "bun" {
					p.Tests = append(p.Tests, "bun run test")
				} else {
					p.Tests = append(p.Tests, "npm test")
				}
			}
		}
	}
	if names["pubspec.yaml"] {
		p.DependencyManagers = append(p.DependencyManagers, "Dart pub")
		b, err := os.ReadFile(filepath.Join(root, "pubspec.yaml"))
		if err != nil {
			return Project{}, fmt.Errorf("read pubspec.yaml: %w", err)
		}
		if bytes.Contains(b, []byte("flutter:")) {
			p.Frameworks = append(p.Frameworks, "Flutter")
			if names["test"] {
				p.Tests = append(p.Tests, "flutter test")
			}
		} else if names["test"] {
			p.Tests = append(p.Tests, "dart test")
		}
	}
	if names["Cargo.toml"] {
		p.DependencyManagers = append(p.DependencyManagers, "Cargo")
		data, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
		if err != nil {
			return Project{}, fmt.Errorf("read Cargo.toml: %w", err)
		}
		text := strings.ToLower(string(data))
		for _, name := range []string{"tokio", "axum", "actix-web", "serde", "tauri"} {
			if strings.Contains(text, name) {
				p.Frameworks = append(p.Frameworks, name)
			}
		}
	}
	if names["pyproject.toml"] || names["requirements.txt"] {
		name := "pyproject.toml"
		if !names[name] {
			name = "requirements.txt"
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return Project{}, fmt.Errorf("read %s: %w", name, err)
		}
		text := strings.ToLower(string(data))
		if name == "requirements.txt" {
			p.DependencyManagers = append(p.DependencyManagers, "pip requirements")
		} else if names["uv.lock"] {
			p.DependencyManagers = append(p.DependencyManagers, "uv")
		} else if strings.Contains(text, "[tool.poetry]") || strings.Contains(text, "poetry-core") {
			p.DependencyManagers = append(p.DependencyManagers, "Poetry")
		} else {
			p.DependencyManagers = append(p.DependencyManagers, "pyproject.toml (manager unspecified)")
		}
		for _, framework := range []string{"django", "fastapi", "flask", "pytest"} {
			if strings.Contains(text, framework) {
				if framework == "pytest" {
					p.Tests = append(p.Tests, "pytest")
				} else {
					p.Frameworks = append(p.Frameworks, framework)
				}
			}
		}
	}
	if names["pom.xml"] || names["build.gradle"] || names["build.gradle.kts"] {
		name := "pom.xml"
		if !names[name] {
			if names["build.gradle"] {
				name = "build.gradle"
			} else {
				name = "build.gradle.kts"
			}
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return Project{}, fmt.Errorf("read %s: %w", name, err)
		}
		text := strings.ToLower(string(data))
		if name == "pom.xml" {
			p.DependencyManagers = append(p.DependencyManagers, "Maven")
		} else {
			p.DependencyManagers = append(p.DependencyManagers, "Gradle")
		}
		if strings.Contains(text, "spring-boot") || strings.Contains(text, "springframework") {
			p.Frameworks = append(p.Frameworks, "Spring")
		}
	}
	if names["tsconfig.json"] && !names["package.json"] {
		p.Languages = append(p.Languages, "TypeScript")
	}
	if names["go.mod"] {
		p.DependencyManagers = append(p.DependencyManagers, "Go modules")
		p.Build = append(p.Build, "go build ./...")
		p.Tests = append(p.Tests, "go test ./...")
	}
	if names["Cargo.toml"] {
		p.Build = append(p.Build, "cargo build")
		p.Tests = append(p.Tests, "cargo test")
	}
	if names["pyproject.toml"] {
		p.Build = append(p.Build, "unknown (pyproject.toml detected)")
		if names["pytest.ini"] || names["tox.ini"] {
			p.Tests = append(p.Tests, "pytest")
		}
	}
	if names["pom.xml"] {
		if runtime.GOOS == "windows" && names["mvnw.cmd"] {
			p.Build = append(p.Build, "mvnw.cmd package")
			p.Tests = append(p.Tests, "mvnw.cmd test")
		} else if names["mvnw"] {
			p.Build = append(p.Build, "./mvnw package")
			p.Tests = append(p.Tests, "./mvnw test")
		} else {
			p.Build = append(p.Build, "mvn package")
			p.Tests = append(p.Tests, "mvn test")
		}
	}
	if names["build.gradle"] || names["build.gradle.kts"] {
		if runtime.GOOS == "windows" && names["gradlew.bat"] {
			p.Tests = append(p.Tests, "gradlew.bat test")
		} else if names["gradlew"] {
			p.Tests = append(p.Tests, "./gradlew test")
		} else {
			p.Tests = append(p.Tests, "gradle test")
		}
	}
	if names["CMakeLists.txt"] {
		b, err := os.ReadFile(filepath.Join(root, "CMakeLists.txt"))
		if err != nil {
			return Project{}, fmt.Errorf("read CMakeLists.txt: %w", err)
		}
		if bytes.Contains(b, []byte("Qt5")) || bytes.Contains(b, []byte("Qt6")) {
			p.Frameworks = append(p.Frameworks, "Qt")
		}
	}
	for name := range names {
		if strings.HasSuffix(name, ".csproj") {
			p.DependencyManagers = append(p.DependencyManagers, "NuGet (.csproj)")
			p.Tests = append(p.Tests, "dotnet test")
		}
	}
	if names["pytest.ini"] || names["tox.ini"] {
		p.Tests = append(p.Tests, "pytest")
	}
	if names["requirements.txt"] {
		b, err := os.ReadFile(filepath.Join(root, "requirements.txt"))
		if err != nil {
			return Project{}, fmt.Errorf("read requirements.txt: %w", err)
		}
		if bytes.Contains(bytes.ToLower(b), []byte("pytest")) {
			p.Tests = append(p.Tests, "pytest")
		}
	}
	for _, candidate := range []string{"main.go", "main.py", "main.rs", "main.cpp", "index.js", "index.ts", "Program.cs", "cmd/main.go", "src/main.go", "src/main.py", "src/main.rs", "src/main.dart", "src/main.js", "src/main.ts", "src/main.tsx", "src/index.js", "src/index.ts", "src/App.tsx", "lib/main.dart", "src/Program.cs", "src/main.cpp"} {
		if info, err := os.Stat(filepath.Join(root, candidate)); err == nil && info.Mode().IsRegular() {
			p.EntryPoints = append(p.EntryPoints, candidate)
		} else if err != nil && !os.IsNotExist(err) {
			return Project{}, fmt.Errorf("inspect entry point %s: %w", candidate, err)
		}
	}
	if commands, err := os.ReadDir(filepath.Join(root, "cmd")); err == nil {
		for _, command := range commands {
			if !command.IsDir() {
				continue
			}
			candidate := filepath.Join("cmd", command.Name(), "main.go")
			if info, statErr := os.Stat(filepath.Join(root, candidate)); statErr == nil && info.Mode().IsRegular() {
				p.EntryPoints = append(p.EntryPoints, filepath.ToSlash(candidate))
			} else if statErr != nil && !os.IsNotExist(statErr) {
				return Project{}, fmt.Errorf("inspect entry point %s: %w", candidate, statErr)
			}
		}
	} else if !os.IsNotExist(err) {
		return Project{}, fmt.Errorf("read cmd directory: %w", err)
	}
	for _, d := range []string{"src", "app", "cmd", "lib", "internal", "pkg", "test", "tests"} {
		if names[d] {
			p.Sources = append(p.Sources, d)
		}
	}
	p.Languages = uniqueStrings(p.Languages)
	p.Frameworks = uniqueStrings(p.Frameworks)
	p.Build = uniqueStrings(p.Build)
	p.Tests = uniqueStrings(p.Tests)
	p.DependencyManagers = uniqueStrings(p.DependencyManagers)
	p.Configurations = uniqueStrings(p.Configurations)
	p.AgentInstructions = uniqueStrings(p.AgentInstructions)
	p.Indexers = uniqueStrings(p.Indexers)
	p.GitConventions = uniqueStrings(p.GitConventions)
	p.PlatformRequirements = uniqueStrings(p.PlatformRequirements)
	sort.Strings(p.Languages)
	sort.Strings(p.Frameworks)
	sort.Strings(p.Sources)
	sort.Strings(p.EntryPoints)
	sort.Strings(p.DependencyManagers)
	sort.Strings(p.Configurations)
	sort.Strings(p.AgentInstructions)
	sort.Strings(p.Indexers)
	sort.Strings(p.GitConventions)
	sort.Strings(p.PlatformRequirements)
	return p, nil
}

func collectRepositoryMetadata(root string, names map[string]bool, p *Project) error {
	p.Configurations = selectNames(names, []string{
		".editorconfig", ".prettierrc", ".prettierrc.json", ".prettierrc.yml", ".prettierrc.yaml", "prettier.config.js", "prettier.config.cjs", "prettier.config.mjs",
		".eslintrc", ".eslintrc.json", ".eslintrc.js", ".eslintrc.cjs", "eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "biome.json", "biome.jsonc",
		".golangci.yml", ".golangci.yaml", "rustfmt.toml", ".rustfmt.toml", "ruff.toml", ".ruff.toml", "mypy.ini", "pytest.ini", "tox.ini", "pyproject.toml", ".clang-format", ".clang-tidy",
	})
	for name := range names {
		if strings.HasPrefix(name, ".prettierrc") || strings.HasPrefix(name, ".eslintrc") || strings.HasPrefix(name, "biome.") {
			p.Configurations = append(p.Configurations, name)
		}
	}
	p.Indexers = selectNames(names, []string{".serena", ".graphify", "graphify.yml", "graphify.yaml", "graphify.json", "mcp.json", ".mcp.json"})
	p.GitConventions = selectNames(names, []string{".gitignore", ".gitattributes", ".gitmodules", ".mailmap", ".gitmessage"})
	p.PlatformRequirements = selectNames(names, []string{"Dockerfile", "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml", "Makefile", "Vagrantfile"})
	for _, path := range []string{".github/workflows", ".devcontainer"} {
		exists, err := pathExists(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if exists {
			p.PlatformRequirements = append(p.PlatformRequirements, path)
		}
	}
	for _, path := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", ".clinerules", ".aider.conf.yml", ".github/copilot-instructions.md"} {
		if !names[filepath.Base(filepath.FromSlash(path))] && path != ".github/copilot-instructions.md" {
			continue
		}
		if path == ".github/copilot-instructions.md" {
			if exists, err := pathExists(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				return fmt.Errorf("inspect %s: %w", path, err)
			} else if !exists {
				continue
			}
		}
		if err := appendUnmanagedInstruction(root, path, p); err != nil {
			return err
		}
	}
	for _, dir := range []string{".cursor/rules", ".windsurf/rules", ".claude/rules", ".cline/rules", ".github/instructions"} {
		fullDir := filepath.Join(root, filepath.FromSlash(dir))
		files, err := os.ReadDir(fullDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read agent instruction directory %s: %w", dir, err)
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			path := filepath.ToSlash(filepath.Join(dir, file.Name()))
			if err := appendUnmanagedInstruction(root, path, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendUnmanagedInstruction(root, relative string, p *Project) error {
	fullPath := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("inspect agent instruction %s: %w", relative, err)
	}
	if info.IsDir() {
		files, err := os.ReadDir(fullPath)
		if err != nil {
			return fmt.Errorf("read agent instruction directory %s: %w", relative, err)
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			child := filepath.ToSlash(filepath.Join(relative, file.Name()))
			if err := appendUnmanagedInstruction(root, child, p); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read agent instruction %s: %w", relative, err)
	}
	text := string(data)
	if strings.Contains(text, "<!-- ai-init:begin -->") || strings.Contains(text, "<!-- ai-init adapter:begin -->") || strings.Contains(text, "# ai-init managed instructions") {
		return nil
	}
	p.AgentInstructions = append(p.AgentInstructions, relative)
	return nil
}

func selectNames(names map[string]bool, candidates []string) []string {
	var found []string
	for _, candidate := range candidates {
		if names[candidate] {
			found = append(found, candidate)
		}
	}
	return found
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
