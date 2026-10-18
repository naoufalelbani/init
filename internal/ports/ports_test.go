package ports

import (
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
