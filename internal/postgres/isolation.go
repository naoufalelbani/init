// Package postgres implements the deliberately narrow PostgreSQL support used
// for opt-in task database isolation. It delegates protocol handling to the
// official PostgreSQL client tools rather than implementing the wire protocol.
package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var taskPart = regexp.MustCompile(`[^a-z0-9]+`)
var roleName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var databaseSuffix = regexp.MustCompile(`^[a-f0-9]{16}$`)
var environmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var databaseNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Connection contains only the fields needed to create/drop one database.
// Password is held in memory and is never placed in process arguments.
type Connection struct {
	Host, Port, User, Owner, Password, MaintenanceDB, SSLMode string
	OmitEnvironment                                           []string
}

// Metadata is safe to keep in the common Git metadata directory; it contains
// no credential or connection URL.
type Metadata struct {
	Version     int    `json:"version"`
	Task        string `json:"task"`
	Database    string `json:"database"`
	Environment string `json:"environment"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	User        string `json:"user"`
}

// ParsePair validates that the target and administrator URLs refer to the
// same simple TCP PostgreSQL endpoint. Errors intentionally omit URL values.
func ParsePair(targetRaw, adminRaw string) (target, admin *url.URL, adminConn Connection, err error) {
	target, err = parseURL(targetRaw)
	if err != nil {
		return nil, nil, Connection{}, errors.New("target database URL is not a supported PostgreSQL URL")
	}
	admin, err = parseURL(adminRaw)
	if err != nil {
		return nil, nil, Connection{}, errors.New("administrator URL is not a supported PostgreSQL URL")
	}
	th, tp := endpoint(target)
	ah, ap := endpoint(admin)
	if !strings.EqualFold(th, ah) || tp != ap {
		return nil, nil, Connection{}, errors.New("target and administrator URLs must use the same host and port")
	}
	if target.User == nil || target.User.Username() == "" {
		return nil, nil, Connection{}, errors.New("target database URL must include a username")
	}
	if !roleName.MatchString(target.User.Username()) {
		return nil, nil, Connection{}, errors.New("target database username must be a simple PostgreSQL role name")
	}
	if strings.Trim(target.EscapedPath(), "/") == "" || strings.Contains(strings.Trim(target.EscapedPath(), "/"), "/") {
		return nil, nil, Connection{}, errors.New("target database URL must name one database")
	}
	if admin.User == nil || admin.User.Username() == "" {
		return nil, nil, Connection{}, errors.New("administrator URL must include a username")
	}
	adminConn, err = ParseAdminURL(adminRaw)
	if err != nil {
		return nil, nil, Connection{}, err
	}
	if target.Query().Get("sslmode") != "" && adminConn.SSLMode != "" && target.Query().Get("sslmode") != adminConn.SSLMode {
		return nil, nil, Connection{}, errors.New("target and administrator URLs specify different sslmode options")
	}
	if adminConn.SSLMode == "" {
		adminConn.SSLMode = target.Query().Get("sslmode")
	}
	adminConn.Owner = target.User.Username()
	if err = validateQuery(target); err != nil {
		return nil, nil, Connection{}, err
	}
	return target, admin, adminConn, nil
}

// ParseAdminURL validates and parses a standalone administrator URL.
func ParseAdminURL(raw string) (Connection, error) {
	u, err := parseURL(raw)
	if err != nil {
		return Connection{}, errors.New("administrator URL is not a supported PostgreSQL URL")
	}
	if err = validateQuery(u); err != nil {
		return Connection{}, err
	}
	if u.User == nil || u.User.Username() == "" {
		return Connection{}, errors.New("administrator URL must include a username")
	}
	if !roleName.MatchString(u.User.Username()) {
		return Connection{}, errors.New("administrator username must be a simple PostgreSQL role name")
	}
	maintenance, err := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/"))
	if err != nil || !roleName.MatchString(maintenance) {
		return Connection{}, errors.New("administrator URL must name one maintenance database")
	}
	host, port := endpoint(u)
	password, _ := u.User.Password()
	if strings.ContainsAny(password, "\r\n") {
		return Connection{}, errors.New("administrator URL contains an unsupported credential")
	}
	return Connection{Host: host, Port: port, User: u.User.Username(), Password: password, MaintenanceDB: maintenance, SSLMode: u.Query().Get("sslmode")}, nil
}

func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Opaque != "" {
		return nil, errors.New("unsupported URL")
	}
	host := u.Hostname()
	if host == "" || !hostnamePattern.MatchString(host) || strings.Contains(host, ":") || strings.ContainsAny(host, "\\/\x00\r\n") {
		return nil, errors.New("only single-host TCP endpoints are supported")
	}
	if u.Fragment != "" || u.ForceQuery {
		return nil, errors.New("unsupported URL form")
	}
	if _, err = endpointPort(u); err != nil {
		return nil, err
	}
	return u, nil
}

func validateQuery(u *url.URL) error {
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("database URL has a malformed query string")
	}
	for key, values := range query {
		if len(values) != 1 {
			return errors.New("database URL has duplicate options")
		}
		switch key {
		case "sslmode":
			switch values[0] {
			case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
			default:
				return errors.New("database URL has an unsupported sslmode")
			}
		default:
			return errors.New("database URL contains unsupported options")
		}
	}
	return nil
}

func endpoint(u *url.URL) (string, string) {
	port, _ := endpointPort(u)
	return u.Hostname(), port
}

func endpointPort(u *url.URL) (string, error) {
	p := u.Port()
	if p == "" {
		return "5432", nil
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("database URL has an invalid port")
	}
	return strconv.Itoa(n), nil
}

// NewDatabaseName makes a PostgreSQL-safe, collision-resistant task database
// name within PostgreSQL's 63-byte identifier limit.
func NewDatabaseName(task string) (string, error) {
	base := taskSlug(task)
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate isolated database name: %w", err)
	}
	return "ai_init_" + base + "_" + hex.EncodeToString(random[:]), nil
}

func taskSlug(task string) string {
	base := strings.Trim(taskPart.ReplaceAllString(strings.ToLower(task), "_"), "_")
	if base == "" {
		base = "task"
	}
	if len(base) > 38 {
		base = strings.TrimRight(base[:38], "_")
	}
	return base
}

// ValidateMetadata ensures cleanup can only target the generated database
// namespace for the named task, even if its local record was edited.
func ValidateMetadata(m Metadata, task string) error {
	prefix := "ai_init_" + taskSlug(task) + "_"
	if m.Version != 1 || m.Task != task || !strings.HasPrefix(m.Database, prefix) || !databaseSuffix.MatchString(strings.TrimPrefix(m.Database, prefix)) || !environmentKeyPattern.MatchString(m.Environment) {
		return errors.New("task database metadata is invalid; refusing cleanup")
	}
	if !hostnamePattern.MatchString(m.Host) || !roleName.MatchString(m.User) {
		return errors.New("task database metadata is invalid; refusing cleanup")
	}
	port, err := strconv.Atoi(m.Port)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != m.Port {
		return errors.New("task database metadata is invalid; refusing cleanup")
	}
	return nil
}

// IsolateURL copies the target URL with its database path replaced.
func IsolateURL(target *url.URL, database string) string {
	clone := *target
	clone.Path = "/" + database
	clone.RawPath = ""
	return clone.String()
}

// Fingerprint identifies the expected endpoint and admin user without
// persisting a password or URL.
func Fingerprint(c Connection) Metadata {
	return Metadata{Version: 1, Host: strings.ToLower(c.Host), Port: c.Port, User: c.User}
}

// Create uses createdb with a temporary passfile. The runner is injectable so
// callers can test argument and secret handling without a real database.
func Create(ctx context.Context, c Connection, database string, run Runner) error {
	return execute(ctx, c, database, "createdb", run)
}

// Drop removes the named task database, and must only be called after explicit
// user authorization from the CLI.
func Drop(ctx context.Context, c Connection, database string, run Runner) error {
	return execute(ctx, c, database, "dropdb", run)
}

type Runner func(context.Context, string, []string, []string) error

func ExecRunner(ctx context.Context, command string, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = env
	return cmd.Run()
}

func execute(ctx context.Context, c Connection, database, command string, run Runner) error {
	if run == nil {
		run = ExecRunner
	}
	if !databaseNamePattern.MatchString(database) {
		return errors.New("refusing unsafe database identifier")
	}
	args := []string{"--no-password", "--host=" + c.Host, "--port=" + c.Port, "--username=" + c.User, "--maintenance-db=" + c.MaintenanceDB}
	if command == "createdb" {
		args = append(args, "--template=template0", "--owner="+c.Owner, database)
	} else {
		args = append(args, "--if-exists", database)
	}
	env := filteredEnvironment(c.OmitEnvironment)
	file, err := os.CreateTemp("", "ai-init-pgpass-*")
	if err != nil {
		return errors.New("could not create temporary PostgreSQL credential file")
	}
	passfile := file.Name()
	defer os.Remove(passfile)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return errors.New("could not secure temporary PostgreSQL credential file")
	}
	if c.Password != "" {
		line := strings.Join([]string{escapePassfile(c.Host), c.Port, "*", escapePassfile(c.User), escapePassfile(c.Password)}, ":") + "\n"
		if _, err = file.WriteString(line); err != nil {
			file.Close()
			return errors.New("could not write temporary PostgreSQL credential file")
		}
	}
	if err = file.Close(); err != nil {
		return errors.New("could not close temporary PostgreSQL credential file")
	}
	env = append(env, "PGPASSFILE="+passfile, "PGCONNECT_TIMEOUT=10")
	if c.SSLMode != "" {
		env = append(env, "PGSSLMODE="+c.SSLMode)
	}
	if err := run(ctx, command, args, env); err != nil {
		return fmt.Errorf("PostgreSQL %s failed; verify client tools, privileges, and database state", command)
	}
	return nil
}

func escapePassfile(s string) string { return strings.NewReplacer(`\`, `\\`, `:`, `\:`).Replace(s) }

func filteredEnvironment(omit []string) []string {
	var env []string
	blocked := make(map[string]struct{}, len(omit))
	for _, key := range omit {
		blocked[strings.ToUpper(key)] = struct{}{}
	}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		upperKey := strings.ToUpper(key)
		preserveTLSFile := slices.Contains([]string{"PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY", "PGSSLCRL", "PGSSLCRLDIR"}, upperKey)
		_, explicitlyOmitted := blocked[upperKey]
		if ok && !explicitlyOmitted && (!strings.HasPrefix(upperKey, "PG") || preserveTLSFile) {
			env = append(env, entry)
		}
	}
	return env
}
