package jenny_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/sdboyer/jenny"
)

func TestFSCaseInsensitiveConflicts(t *testing.T) {
	jfs := fsWith(t, *jenny.NewFile("pkg/Types.go", nil, src("first")))

	err := jfs.Add(*jenny.NewFile("pkg/types.go", nil, src("second")))
	if err == nil {
		t.Fatal("Add accepted a path differing only by case from an existing one")
	}
	if !strings.Contains(err.Error(), "differs only by case from pkg/Types.go") {
		t.Errorf("error doesn't name the conflicting path: %s", err)
	}

	other := fsWith(t, *jenny.NewFile("PKG/types.go", nil, src("third")))
	if err := jfs.Merge(other); err == nil {
		t.Error("Merge accepted a path differing only by case from an existing one")
	}

	if jfs.Len() != 1 {
		t.Errorf("failed Add/Merge changed the FS: Len is %d, want 1", jfs.Len())
	}
}

func TestMergeSelf(t *testing.T) {
	jfs := fsWith(t, *jenny.NewFile("a", nil, src("test")))
	// Must return a conflict error rather than deadlock.
	if err := jfs.Merge(jfs); err == nil {
		t.Error("expected conflict error merging an FS into itself")
	}
}

// Run with -race: Merge must read its argument under that FS's lock.
func TestMergeWhileSourceChanges(t *testing.T) {
	src2 := jenny.NewFS()
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			_ = src2.Add(*jenny.NewFile("f"+strings.Repeat("x", i), nil, src("w")))
		}
	})
	for range 50 {
		dst := jenny.NewFS()
		_ = dst.Merge(src2)
	}
	wg.Wait()
}
