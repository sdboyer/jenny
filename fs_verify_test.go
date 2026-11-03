package jenny_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sdboyer/jenny"
)

func TestVerifyErrorOrderIsDeterministic(t *testing.T) {
	fl := make([]jenny.File, 100)
	for i := range fl {
		fl[i] = *jenny.NewFile(fmt.Sprintf("f%03d", i), []byte("x"), jenny.Name("test"))
	}
	jfs := fsWith(t, fl...)
	dir := t.TempDir()
	var first string
	for i := range 5 {
		err := jfs.Verify(context.Background(), dir)
		if err == nil {
			t.Fatal("expected errors")
		}
		if i == 0 {
			first = err.Error()
			if lines := strings.Split(first, "\n"); !slices.IsSorted(lines) {
				t.Fatalf("Verify errors are not in path order:\n%s", first)
			}
		} else if err.Error() != first {
			t.Fatal("Verify error text differs between runs")
		}
	}
}

func TestWriteVerifyHonorContext(t *testing.T) {
	jfs := fsWith(t, *jenny.NewFile("a", []byte("a"), jenny.Name("test")), *jenny.NewFile("b", []byte("b"), jenny.Name("test")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()

	if err := jfs.Write(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Errorf("Write on canceled ctx returned %v, want context.Canceled", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Write on canceled ctx wrote %d entries", len(entries))
	}
	if err := jfs.Verify(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Errorf("Verify on canceled ctx returned %v, want context.Canceled", err)
	}
}

func TestVerifyFSDriftErrors(t *testing.T) {
	jfs := fsWith(t,
		*jenny.NewFile("a/missing.txt", []byte("a"), jenny.Name("test")),
		*jenny.NewFile("b/changed.txt", []byte("new\n"), jenny.Name("test")),
		*jenny.NewFile("c/same.txt", []byte("c"), jenny.Name("test")),
	)
	persisted := fstest.MapFS{
		"b/changed.txt": {Data: []byte("old\n")},
		"c/same.txt":    {Data: []byte("c")},
		"d/extra.txt":   {Data: []byte("not generated")},
	}

	err := jfs.VerifyFS(context.Background(), persisted)
	joined, ok := err.(interface{ Unwrap() []error }) //nolint:errorlint // inspecting the join itself
	if !ok {
		t.Fatalf("VerifyFS returned %T, want joined DriftErrors: %v", err, err)
	}
	var got []jenny.DriftError
	for _, e := range joined.Unwrap() {
		var de *jenny.DriftError
		if !errors.As(e, &de) {
			t.Fatalf("joined error %T is not a *DriftError: %v", e, e)
		}
		got = append(got, *de)
	}
	if len(got) != 2 {
		t.Fatalf("got %d DriftErrors, want 2: %v", len(got), err)
	}
	if want := (jenny.DriftError{Path: "a/missing.txt", Missing: true}); got[0] != want {
		t.Errorf("first DriftError = %+v, want %+v", got[0], want)
	}
	if got[1].Path != "b/changed.txt" || got[1].Missing || !strings.Contains(got[1].Diff, "-") || !strings.Contains(got[1].Diff, "old") || !strings.Contains(got[1].Diff, "new") {
		t.Errorf("second DriftError = %+v, want a diff of b/changed.txt from old to new", got[1])
	}
}

func TestVerifyFSMatches(t *testing.T) {
	jfs := fsWith(t,
		*jenny.NewFile("a.txt", []byte("a"), jenny.Name("test")),
		*jenny.NewFile("d/b.txt", []byte("b"), jenny.Name("test")),
	)
	ctx := context.Background()
	if err := jfs.VerifyFS(ctx, jfs); err != nil {
		t.Errorf("FS doesn't verify against itself: %v", err)
	}
	if err := jfs.VerifyFS(ctx, fstest.MapFS{"a.txt": {Data: []byte("a")}, "d/b.txt": {Data: []byte("b")}}); err != nil {
		t.Errorf("FS doesn't verify against an equal MapFS: %v", err)
	}
}

// errFS fails to read one name with err, and reads the rest from fsys.
type errFS struct {
	fsys fs.FS
	name string
	err  error
}

func (e errFS) Open(name string) (fs.File, error) {
	if name == e.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: e.err}
	}
	return e.fsys.Open(name)
}

// A failure to read the persisted copy is not drift, and replaces any drift
// found, as it would otherwise hide which files could not be checked.
func TestVerifyFSReadErrorIsNotDrift(t *testing.T) {
	jfs := fsWith(t,
		*jenny.NewFile("a.txt", []byte("a"), jenny.Name("test")),
		*jenny.NewFile("b.txt", []byte("b"), jenny.Name("test")),
	)
	errBroken := errors.New("broken")
	err := jfs.VerifyFS(context.Background(), errFS{fsys: fstest.MapFS{}, name: "b.txt", err: errBroken})
	if !errors.Is(err, errBroken) {
		t.Fatalf("VerifyFS returned %v, want the read error", err)
	}
	var de *jenny.DriftError
	if errors.As(err, &de) {
		t.Errorf("read error reported alongside drift %+v", de)
	}
}

// Verify reads the directory through os.DirFS, so DriftError paths are
// slash-separated names relative to the directory, on every OS.
func TestVerifyDriftPathsAreSlashSeparated(t *testing.T) {
	jfs := fsWith(t, *jenny.NewFile("sub/dir/a.txt", []byte("a"), jenny.Name("test")))
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "dir", "a.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	var de *jenny.DriftError
	if err := jfs.Verify(context.Background(), dir); !errors.As(err, &de) {
		t.Fatalf("Verify returned %v, want a DriftError", err)
	}
	if de.Path != "sub/dir/a.txt" || de.Missing {
		t.Errorf("DriftError = %+v, want a diff at sub/dir/a.txt", de)
	}
}

func TestVerifyEmptyPrefixIsWorkingDir(t *testing.T) {
	jfs := fsWith(t, *jenny.NewFile("sub/a.txt", []byte("a"), jenny.Name("test")))
	dir := t.TempDir()
	if err := jfs.Write(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := jfs.Verify(context.Background(), ""); err != nil {
		t.Errorf("Verify with empty prefix didn't read the working directory: %v", err)
	}
}
