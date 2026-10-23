package jenny

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
)

// FS is a pseudo-filesystem that supports batch-writing its contents to the
// real filesystem, or batch-comparing its contents to the real filesystem. Its
// intended use is for idiomatic `go generate`-style code generators, where it
// is expected that the results of codegen are committed to version control.
//
// In such cases, the normal behavior of a generator is to write files to disk,
// but in CI, that behavior should change to verify that what is already on disk
// is identical to the results of code generation. This allows CI to ensure that
// the results of code generation are always up to date. FS supports these
// related behaviors through its [FS.Write] and [FS.Verify] methods, respectively.
//
// FS behaves like an immutable append-only data structure - [File]s may not be
// removed once [FS.Add]ed. If a path conflict occurs when adding a new file or
// merging another FS, an error is returned. Paths that differ only by case
// conflict, since they collide on case-insensitive filesystems.
//
// Every File added to FS must have a relative path. An absolute path may be
// provided as a universal prefix on calls to FS.Write or FS.Verify.
//
// FS implements [io/fs.FS], backed by [fstest.MapFS]. Added mutexes make FS
// safe for concurrent use, but it has the same scaling limitations as
// [fstest.MapFS] for large numbers of files.
//
// Note that the statelessness of FS entails that if a particular Jenny Input
// goes away, FS.Verify cannot know what orphaned generated files should be
// removed.
type FS struct {
	m fstest.MapFS

	// folded maps the lowercased form of each path in m to the path
	// itself, so conflicts can be detected case-insensitively, consistent
	// with [Files.Validate].
	folded map[string]string

	mu sync.RWMutex
}

type jennystack []NamedJenny

func (js jennystack) String() string {
	strs := make([]string, len(js))
	for i, j := range js {
		strs[i] = j.JennyName()
	}
	return strings.Join(strs, ":")
}

// NewFS creates a new FS, ready for use.
func NewFS() *FS {
	return &FS{
		m:      make(fstest.MapFS),
		folded: make(map[string]string),
	}
}

// Verify checks the contents of each file against the filesystem. It emits an error
// if any of its contained files differ, listing every differing file in path
// order.
//
// Verify stops early, returning an error wrapping ctx's error, if ctx is done.
//
// If the provided prefix path is non-empty, it will be prepended to all file
// entries in the map for writing. prefix may be an absolute path.
func (fs *FS) Verify(ctx context.Context, prefix string) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(12)

	files := fs.AsFiles()
	// Indexed by file, so the joined error lists files in path order no matter
	// which goroutine finishes first.
	diffs := make([]error, len(files))

	for i, item := range files {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			ipath := filepath.Join(prefix, item.RelativePath)
			if _, err := os.Stat(ipath); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					diffs[i] = fmt.Errorf("%s: generated file should exist, but does not", ipath)
				} else {
					return fmt.Errorf("%s: could not stat generated file: %w", ipath, err)
				}
				return nil
			}

			ob, err := os.ReadFile(ipath) //nolint:gosec
			if err != nil {
				return fmt.Errorf("%s: error reading file: %w", ipath, err)
			}
			dstr := cmp.Diff(string(ob), string(item.Data))
			if dstr != "" {
				diffs[i] = fmt.Errorf("%s would have changed:\n\n%s", ipath, dstr)
			}
			return nil
		})
	}
	err := g.Wait()
	if err != nil {
		return fmt.Errorf("io error while verifying tree: %w", err)
	}

	return errors.Join(diffs...)
}

// Write writes all of the files to their indicated paths.
//
// If the provided prefix path is non-empty, it will be prepended to all file
// entries in the map for writing. prefix may be an absolute path.
//
// Write stops starting new writes once ctx is done, or once any write fails;
// files already written are left in place.
// TODO try to undo already-written files on error (only best effort, it's impossible to guarantee)
func (fs *FS) Write(ctx context.Context, prefix string) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(12)

	for _, it := range fs.AsFiles() {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			path := filepath.Join(prefix, it.RelativePath)
			err := os.MkdirAll(filepath.Dir(path), os.ModePerm)
			if err != nil {
				return fmt.Errorf("%s: failed to ensure parent directory exists: %w", path, err)
			}

			if err := os.WriteFile(path, it.Data, 0644); err != nil { //nolint:gosec
				return fmt.Errorf("%s: error while writing file: %w", path, err)
			}
			return nil
		})
	}

	return g.Wait()
}

// AsFiles returns a Files representing the contents of the FS.
//
// The contents are sorted lexicographically, and it is guaranteed that the
// invariants enforced by [Files.Validate] are met.
func (fs *FS) AsFiles() []File {
	if fs == nil {
		return nil
	}

	fs.mu.RLock()
	sl := make([]File, 0, len(fs.m))

	for path, fi := range fs.m {
		sl = append(sl, toFile(path, fi))
	}

	sort.Slice(sl, func(i, j int) bool {
		return sl[i].RelativePath < sl[j].RelativePath
	})

	fs.mu.RUnlock()
	return sl
}

// Add adds one or more files to the FS. An error is returned if the
// RelativePath of any provided files already exists in the FS.
func (fs *FS) Add(flist ...File) error {
	fs.mu.Lock()
	err := fs.add(flist...)
	fs.mu.Unlock()
	return err
}

func (fs *FS) add(flist ...File) error {
	if err := Files(flist).Validate(); err != nil {
		return err
	}

	return fs.addValidated(flist...)
}

func (fs *FS) addValidated(flist ...File) error {
	var errs []error

	for _, f := range flist {
		if existing, has := fs.folded[strings.ToLower(f.RelativePath)]; has {
			rf := fs.m[existing]
			if existing == f.RelativePath {
				errs = append(errs, fmt.Errorf("cannot create %s for jenny %q, path already created by jenny %q", f.RelativePath, jennystack(f.From), stack(rf)))
			} else {
				errs = append(errs, fmt.Errorf("cannot create %s for jenny %q, path differs only by case from %s, created by jenny %q", f.RelativePath, jennystack(f.From), existing, stack(rf)))
			}
		} else if filepath.IsAbs(f.RelativePath) {
			errs = append(errs, fmt.Errorf("files must have relative paths, got %s from %q", f.RelativePath, jennystack(f.From)))
		}
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	for _, f := range flist {
		fs.m[f.RelativePath] = &fstest.MapFile{Data: f.Data, Sys: f.From}
		fs.folded[strings.ToLower(f.RelativePath)] = f.RelativePath
	}
	return nil
}

// Merge combines all the entries from the provided FS into the receiver
// FS. Duplicate paths, including paths that differ only by case, result in
// an error.
func (fs *FS) Merge(fs2 *FS) error {
	if fs2 == nil {
		return nil
	}

	// Snapshot fs2 under its own read lock before taking fs's write lock, so
	// that fs2 can't change mid-read, and so fs.Merge(fs) can't deadlock.
	flist := fs2.AsFiles()

	fs.mu.Lock()
	defer fs.mu.Unlock()

	return fs.add(flist...)
}

// Len returns the number of items in the FS.
func (fs *FS) Len() int {
	fs.mu.Lock()
	ret := len(fs.m)
	fs.mu.Unlock()
	return ret
}

// FileMapper takes a File and transforms it into a new File.
//
// jenny generally assumes that FileMappers will reuse an
// unmodified byte slice.
type FileMapper func(File) (File, error)

// Map creates a new FS by passing each [File] element in the receiver FS
// through the provided [FileMapper].
func (fs *FS) Map(fn FileMapper) (*FS, error) {
	flist := fs.AsFiles()
	nflist := make([]File, 0, len(flist))
	for _, file := range flist {
		nf, err := fn(file)
		if err != nil {
			return nil, err
		}
		nflist = append(nflist, nf)
	}
	fs2 := NewFS()
	if err := fs2.add(nflist...); err != nil {
		return nil, err
	}
	return fs2, nil
}

func toFile(path string, mf *fstest.MapFile) File {
	return File{
		RelativePath: path,
		Data:         mf.Data,
		From:         mf.Sys.([]NamedJenny),
	}
}

func stack(mf *fstest.MapFile) jennystack {
	return jennystack(mf.Sys.([]NamedJenny))
}
