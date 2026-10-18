package ports

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Registry struct {
	Allocations map[string]map[string]int `json:"allocations"`
}

var serviceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func validTask(task string) bool {
	return task != "" && task != "." && task != ".." && filepath.Base(task) == task && !strings.ContainsAny(task, "/\\")
}

func Allocate(root, task, service string) (int, error) {
	if !validTask(task) || !serviceName.MatchString(service) {
		return 0, errors.New("task and service names must contain only letters, digits, hyphens, and underscores")
	}
	state, err := statePaths(root)
	if err != nil {
		return 0, err
	}
	if err = acquire(state.lock); err != nil {
		return 0, err
	}
	defer os.Remove(state.lock)
	registry, err := read(state.file)
	if err != nil {
		return 0, err
	}
	if existing := registry.Allocations[task][service]; existing > 0 {
		return existing, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find available port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		return 0, err
	}
	for _, services := range registry.Allocations {
		for _, allocated := range services {
			if allocated == port {
				return 0, errors.New("selected port is already present in project allocation records; retry")
			}
		}
	}
	if registry.Allocations[task] == nil {
		registry.Allocations[task] = map[string]int{}
	}
	registry.Allocations[task][service] = port
	if err = write(state.file, registry); err != nil {
		return 0, err
	}
	return port, nil
}

func Preview(root, task, service string) (int, error) {
	if !validTask(task) || !serviceName.MatchString(service) {
		return 0, errors.New("invalid task or service name")
	}
	state, err := statePaths(root)
	if err != nil {
		return 0, err
	}
	registry, err := read(state.file)
	if err != nil {
		return 0, err
	}
	if existing := registry.Allocations[task][service]; existing > 0 {
		return existing, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}
func Release(root, task string) error {
	if !validTask(task) {
		return errors.New("invalid task name")
	}
	state, err := statePaths(root)
	if err != nil {
		return err
	}
	if err = acquire(state.lock); err != nil {
		return err
	}
	defer os.Remove(state.lock)
	registry, err := read(state.file)
	if err != nil {
		return err
	}
	delete(registry.Allocations, task)
	return write(state.file, registry)
}

func ReleaseService(root, task, service string) error {
	if !validTask(task) || !serviceName.MatchString(service) {
		return errors.New("invalid task or service name")
	}
	state, err := statePaths(root)
	if err != nil {
		return err
	}
	if err = acquire(state.lock); err != nil {
		return err
	}
	defer os.Remove(state.lock)
	registry, err := read(state.file)
	if err != nil {
		return err
	}
	delete(registry.Allocations[task], service)
	if len(registry.Allocations[task]) == 0 {
		delete(registry.Allocations, task)
	}
	return write(state.file, registry)
}
func List(root string) (Registry, error) {
	state, err := statePaths(root)
	if err != nil {
		return Registry{}, err
	}
	return read(state.file)
}
func statePaths(root string) (struct{ file, lock string }, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return struct{ file, lock string }{}, err
	}
	dir := filepath.Join(abs, ".ai-init")
	return struct{ file, lock string }{filepath.Join(dir, "ports.json"), filepath.Join(dir, "ports.lock")}, nil
}
func read(path string) (Registry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Registry{Allocations: map[string]map[string]int{}}, nil
	}
	if err != nil {
		return Registry{}, err
	}
	var value Registry
	if err = json.Unmarshal(data, &value); err != nil {
		return Registry{}, fmt.Errorf("read port registry %s: %w", path, err)
	}
	if value.Allocations == nil {
		value.Allocations = map[string]map[string]int{}
	}
	return value, nil
}
func write(path string, value Registry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + fmt.Sprintf(".tmp-%d", os.Getpid())
	if err = os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err == nil {
		return nil
	}
	old := path + fmt.Sprintf(".replace-%d", os.Getpid())
	if err = os.Rename(path, old); err != nil {
		_ = os.Remove(tmp)
		if os.IsNotExist(err) {
			return os.Rename(tmp, path)
		}
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		_ = os.Rename(old, path)
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(old)
	return nil
}
func acquire(lock string) error {
	if err := os.MkdirAll(filepath.Dir(lock), 0755); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(lock, 0700)
		if err == nil {
			return nil
		}
		if !os.IsExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for port allocation lock %s; inspect and remove it only if no ai-init process is using it", lock)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func SortedTasks(value Registry) []string {
	tasks := make([]string, 0, len(value.Allocations))
	for task := range value.Allocations {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)
	return tasks
}

func SortedServices(value Registry, task string) []string {
	services := make([]string, 0, len(value.Allocations[task]))
	for service := range value.Allocations[task] {
		services = append(services, service)
	}
	sort.Strings(services)
	return services
}
