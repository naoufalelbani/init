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
		if names["pnpm-lock.yaml"] {
			manager = "pnpm"
		} else if names["yarn.lock"] {
			manager = "yarn"
		} else if names["bun.lockb"] || names["bun.lock"] {
			manager = "bun"
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
		if strings.Contains(text, "spring-boot") || strings.Contains(text, "springframework") {
			p.Frameworks = append(p.Frameworks, "Spring")
		}
	}
	if names["tsconfig.json"] && !names["package.json"] {
		p.Languages = append(p.Languages, "TypeScript")
	}
	if names["go.mod"] {
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
	sort.Strings(p.Languages)
	sort.Strings(p.Frameworks)
	sort.Strings(p.Sources)
	sort.Strings(p.EntryPoints)
	return p, nil
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
