package environment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestCaptureOnlyAllowlistedVariablesAndSortKeys(t *testing.T) {
	lookup := func(key string) (string, bool) {
		values := map[string]string{"TOKEN": "secret $ value", "PORT": "9000", "UNSELECTED": "must not leak"}
		value, ok := values[key]
		return value, ok
	}
	values, keys, err := Capture([]string{"TOKEN", "PORT", "TOKEN"}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, []string{"PORT", "TOKEN"}) {
		t.Fatalf("keys=%v", keys)
	}
	if len(values) != 2 || values["UNSELECTED"] != "" {
		t.Fatalf("captured unexpected values: %#v", values)
	}
	data, err := Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["TOKEN"] != "secret $ value" || len(decoded) != 2 {
		t.Fatalf("decoded=%#v", decoded)
	}
}

func TestCaptureRejectsMissingAndMalformedKeys(t *testing.T) {
	lookup := func(string) (string, bool) { return "", false }
	for _, keys := range [][]string{{"MISSING"}, {"BAD=NAME"}, {"1BAD"}} {
		if _, _, err := Capture(keys, lookup); err == nil {
			t.Fatalf("expected invalid key or missing variable error for %q", keys)
		}
	}
}

func TestDatabaseConnectionKeysFindsLikelySharedDataEndpoints(t *testing.T) {
	keys := DatabaseConnectionKeys([]string{"API_TOKEN", "DATABASE_URL", "APP_DB_HOST", "PGHOST", "REDIS_URL", "PORT"})
	want := []string{"APP_DB_HOST", "DATABASE_URL", "PGHOST", "REDIS_URL"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("database connection keys=%v, want %v", keys, want)
	}
}

func TestWriteSnapshotIsPrivateAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.env.json")
	if err := WriteSnapshot(path, map[string]string{"TOKEN": "value"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o, want 600", info.Mode().Perm())
	}
	if err = WriteSnapshot(path, map[string]string{"TOKEN": "replacement"}); err == nil {
		t.Fatal("existing environment snapshot was overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["TOKEN"] != "value" {
		t.Fatalf("existing file changed: %#v", got)
	}
}
