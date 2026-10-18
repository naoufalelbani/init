package ports

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestConcurrentAllocationsAreUniqueAndRecorded(t *testing.T) {
	root := t.TempDir()
	const n = 12
	values := make(chan int, n)
	errs := make(chan error, n)
	var group sync.WaitGroup
	for i := 0; i < n; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			port, err := Allocate(root, "task", "service"+string(rune('a'+i)))
			values <- port
			errs <- err
		}(i)
	}
	group.Wait()
	close(values)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[int]bool{}
	for port := range values {
		if seen[port] {
			t.Fatalf("duplicate port %d", port)
		}
		seen[port] = true
	}
	registry, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Allocations["task"]) != n {
		t.Fatalf("allocations=%v", registry.Allocations)
	}
}

func TestLockContentionRecognizesWindowsAccessDeniedWhenDirectoryExists(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "ports.lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if !isLockContention(os.ErrPermission, lock) {
		t.Fatal("existing lock directory was not treated as contention")
	}
	missingLock := filepath.Join(t.TempDir(), "missing.lock")
	contention := isLockContention(os.ErrPermission, missingLock)
	if runtime.GOOS == "windows" && !contention {
		t.Fatal("Windows access-denied collision was not retried")
	}
	if runtime.GOOS != "windows" && contention {
		t.Fatal("permission failure without an existing lock was treated as contention")
	}
}

func TestAllocationIsIdempotentAndReleaseRemovesIt(t *testing.T) {
	root := t.TempDir()
	first, err := Allocate(root, "feature", "http")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Allocate(root, "feature", "http")
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatalf("allocation changed: %d to %d", first, again)
	}
	if err = Release(root, "feature"); err != nil {
		t.Fatal(err)
	}
	registry, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Allocations) != 0 {
		t.Fatalf("release left records: %v", registry.Allocations)
	}
}

func TestPreviewDoesNotRecordAllocation(t *testing.T) {
	root := t.TempDir()
	port, err := Preview(root, "feature", "web")
	if err != nil {
		t.Fatal(err)
	}
	if port < 1 {
		t.Fatalf("invalid preview port %d", port)
	}
	registry, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Allocations) != 0 {
		t.Fatalf("preview mutated records: %v", registry.Allocations)
	}
}
