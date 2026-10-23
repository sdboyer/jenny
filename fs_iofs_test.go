package jenny_test

import (
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/sdboyer/jenny"
)

func TestFSConformsToIOFS(t *testing.T) {
	jfs := fsWith(t,
		*jenny.NewFile("a.txt", []byte("a"), src("test")),
		*jenny.NewFile("d/b.txt", []byte("b"), src("test")),
		*jenny.NewFile("d/e/c.txt", []byte("c"), src("test")),
	)
	if err := fstest.TestFS(jfs, "a.txt", "d/b.txt", "d/e/c.txt"); err != nil {
		t.Fatal(err)
	}
	sub, err := fs.Sub(jfs, "d")
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(sub, "b.txt", "e/c.txt"); err != nil {
		t.Fatal(err)
	}
}

// Run with -race: every io/fs method must be safe to call during FS.Add.
func TestFSReadsDuringAdd(t *testing.T) {
	jfs := fsWith(t, *jenny.NewFile("d/seed", []byte("x"), src("test")))
	sub, err := jfs.Sub("d")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			_ = jfs.Add(*jenny.NewFile("d/f"+strings.Repeat("x", i), []byte("x"), src("test")))
		}
	})
	for range 100 {
		_, _ = jfs.Open("d/seed")
		_, _ = jfs.ReadFile("d/seed")
		_, _ = jfs.Stat("d")
		_, _ = jfs.ReadDir("d")
		_, _ = jfs.Glob("d/*")
		_, _ = fs.ReadDir(sub, ".")
	}
	wg.Wait()
}
