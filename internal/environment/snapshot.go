package environment

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Capture returns only the environment variables explicitly named by the caller.
func Capture(keys []string, lookup func(string) (string, bool)) (map[string]string, []string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if !keyPattern.MatchString(key) {
			return nil, nil, fmt.Errorf("invalid environment variable name %q", key)
		}
		if _, exists := values[key]; exists {
			continue
		}
		value, exists := lookup(key)
		if !exists {
			return nil, nil, fmt.Errorf("environment variable %s is not set", key)
		}
		values[key] = value
	}
	ordered := make([]string, 0, len(values))
	for key := range values {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	return values, ordered, nil
}

func CaptureProcess(keys []string) (map[string]string, []string, error) {
	return Capture(keys, os.LookupEnv)
}

// DatabaseConnectionKeys identifies explicitly selected environment keys that
// are likely to carry credentials or endpoints for shared data services.
func DatabaseConnectionKeys(keys []string) []string {
	var matches []string
	for _, key := range keys {
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "DATABASE") || strings.Contains(upper, "_DB_") || strings.HasSuffix(upper, "_DB") {
			matches = append(matches, key)
			continue
		}
		for _, prefix := range []string{
			"DB_", "DBURL", "DBURI", "DBHOST", "DBPORT", "DBNAME", "POSTGRES", "POSTGRESQL", "MYSQL", "MARIADB", "MONGO", "MONGODB", "REDIS", "ELASTICSEARCH", "DATASOURCE", "JDBC", "SQLSERVER", "MSSQL", "ORACLE", "COCKROACH", "CLICKHOUSE", "SNOWFLAKE",
			"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD",
		} {
			if strings.HasPrefix(upper, prefix) {
				matches = append(matches, key)
				break
			}
		}
	}
	sort.Strings(matches)
	return matches
}

func Marshal(values map[string]string) ([]byte, error) {
	for key := range values {
		if !keyPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid environment variable name %q", key)
		}
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// WriteSnapshot creates a task-local JSON file and never overwrites an existing file.
func WriteSnapshot(path string, values map[string]string) error {
	if len(values) == 0 {
		return errors.New("refusing to write an empty environment snapshot")
	}
	data, err := Marshal(values)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
