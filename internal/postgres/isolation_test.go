package postgres

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestParsePairAndIsolateURL(t *testing.T) {
	target, _, admin, err := ParsePair("postgres://app:p%40ss@db.example:5432/shared?sslmode=require", "postgresql://admin:secret@DB.example/postgres?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	if admin.Password != "secret" || admin.SSLMode != "require" || admin.Port != "5432" {
		t.Fatalf("parsed administrator connection = %#v", admin)
	}
	got := IsolateURL(target, "ai_init_feature_0123456789abcdef")
	if !strings.Contains(got, "/ai_init_feature_0123456789abcdef") || !strings.Contains(got, "sslmode=require") {
		t.Fatalf("isolated URL = %q", got)
	}
}

func TestParsePairRejectsUnsafeOrDifferentTargetsWithoutEchoingSecrets(t *testing.T) {
	for _, pair := range [][2]string{
		{"postgres://app:TOPSECRET@host/shared", "postgres://admin:OTHERSECRET@other/postgres"},
		{"postgres://app:TOPSECRET@host/shared?host=other", "postgres://admin:OTHERSECRET@host/postgres"},
		{"postgres://app:TOPSECRET@host/shared?password=leak", "postgres://admin:OTHERSECRET@host/postgres"},
	} {
		_, _, _, err := ParsePair(pair[0], pair[1])
		if err == nil {
			t.Fatalf("ParsePair unexpectedly accepted %q", pair[0])
		}
		if strings.Contains(err.Error(), "TOPSECRET") || strings.Contains(err.Error(), "OTHERSECRET") {
			t.Fatalf("error exposed credential: %v", err)
		}
	}
}

func TestNewDatabaseNameAndClientInvocationDoNotExposePassword(t *testing.T) {
	name, err := NewDatabaseName("Feature: 日本語 name with a deliberately long suffix")
	if err != nil {
		t.Fatal(err)
	}
	if !regexpDatabase.MatchString(name) {
		t.Fatalf("unsafe generated database name %q", name)
	}
	_, _, conn, err := ParsePair("postgres://app@host/shared", "postgres://admin:s%3Acret@host/postgres")
	if err != nil {
		t.Fatal(err)
	}
	var gotArgs, gotEnv []string
	err = Create(context.Background(), conn, name, func(_ context.Context, command string, args, env []string) error {
		if command != "createdb" {
			t.Fatalf("command = %q", command)
		}
		gotArgs, gotEnv = append([]string(nil), args...), append([]string(nil), env...)
		for _, value := range env {
			if strings.HasPrefix(value, "PGPASSFILE=") {
				data, err := os.ReadFile(strings.TrimPrefix(value, "PGPASSFILE="))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "host:5432:*:admin:s\\:cret\n" {
					t.Fatalf("unexpected passfile content %q", string(data))
				}
				info, err := os.Stat(strings.TrimPrefix(value, "PGPASSFILE="))
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
					t.Fatalf("passfile permissions %o, want 600", info.Mode().Perm())
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range gotArgs {
		if strings.Contains(arg, "s:cret") {
			t.Fatalf("password leaked in argument %q", arg)
		}
	}
	joinedEnv := strings.Join(gotEnv, "\n")
	if strings.Contains(joinedEnv, "PGPASSWORD=") || strings.Contains(joinedEnv, "s:cret") {
		t.Fatalf("password leaked in environment: %s", joinedEnv)
	}
	if !strings.Contains(joinedEnv, "PGPASSFILE=") {
		t.Fatal("credential file was not selected")
	}
	if len(name) > 63 {
		t.Fatalf("database name length = %d, exceeds PostgreSQL limit", len(name))
	}
}

func TestValidateMetadataRestrictsDropToTaskNamespace(t *testing.T) {
	metadata := Metadata{
		Version: 1, Task: "Feature name", Database: "ai_init_feature_name_0123456789abcdef",
		Environment: "DATABASE_URL", Host: "db.example", Port: "5432", User: "db_admin",
	}
	if err := ValidateMetadata(metadata, "Feature name"); err != nil {
		t.Fatalf("valid task database metadata rejected: %v", err)
	}
	metadata.Database = "shared_production"
	if err := ValidateMetadata(metadata, "Feature name"); err == nil {
		t.Fatal("cleanup metadata accepted a database outside the task namespace")
	}
}

var regexpDatabase = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
