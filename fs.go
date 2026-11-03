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
// FS implements [io/fs.FS], backed by a copy of [testing/fstest.MapFS]. Added
// mutexes make FS safe for concurrent use, but it has the same scaling
// limitations as [testing/fstest.MapFS] for large numbers of files.
//
// FS holds files as bytes: a [Tree] is rendered when it's added.
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

// Verify is [FS.VerifyFS] against the directory prefix, read through
// [os.DirFS]. prefix may be an absolute path; an empty prefix is the current
// directory.
func (fs *FS) Verify(ctx context.Context, prefix string) error {
	if prefix == "" {
		prefix = "."
	}
	return fs.VerifyFS(ctx, os.DirFS(prefix))
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

	for _, it := range fs.contents() {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			local, err := filepath.Localize(it.Path())
			if err != nil {
				return fmt.Errorf("%s: cannot be written on this OS: %w", it.Path(), err)
			}
			path := filepath.Join(prefix, local)
			err = os.MkdirAll(filepath.Dir(path), os.ModePerm)
			if err != nil {
				return fmt.Errorf("%s: failed to ensure parent directory exists: %w", path, err)
			}

			if err := os.WriteFile(path, it.data, 0644); err != nil { //nolint:gosec
				return fmt.Errorf("%s: error while writing file: %w", path, err)
			}
			return nil
		})
	}

	return g.Wait()
}

// AsFiles returns a Files representing the contents of the FS, each as
// [Bytes].
//
// The contents are sorted lexicographically, and it is guaranteed that the
// invariants enforced by [Files.Validate] are met.
func (fs *FS) AsFiles() []File {
	if fs == nil {
		return nil
	}
	contents := fs.contents()
	sl := make([]File, len(contents))
	for i, b := range contents {
		sl[i] = b
	}
	return sl
}

// contents returns the files in the FS, sorted by path.
func (fs *FS) contents() []Bytes {
	fs.mu.RLock()
	sl := make([]Bytes, 0, len(fs.m))
	for path, fi := range fs.m {
		sl = append(sl, toFile(path, fi))
	}
	fs.mu.RUnlock()

	sort.Slice(sl, func(i, j int) bool {
		return sl[i].path < sl[j].path
	})
	return sl
}

// Add adds one or more files to the FS, rendering each [Tree]. An error is
// returned if the path of any provided files already exists in the FS, or if
// one fails to render.
func (fs *FS) Add(flist ...File) error {
	rendered, err := render(flist)
	if err != nil {
		return err
	}
	fs.mu.Lock()
	err = fs.addRendered(rendered...)
	fs.mu.Unlock()
	return err
}

// render validates flist, and renders each File in it to Bytes. It runs
// before taking the FS's lock, since rendering may be slow.
func render(flist []File) ([]Bytes, error) {
	if err := Files(flist).Validate(); err != nil {
		return nil, err
	}

	var errs []error
	out := make([]Bytes, len(flist))
	for i, f := range flist {
		f = normalize(f)
		if b, ok := f.(Bytes); ok {
			out[i] = b
			continue
		}
		data, err := f.Render()
		if err != nil {
			errs = append(errs, fmt.Errorf("rendering %s from %q failed: %w", f.Path(), jennystack(f.From()), err))
			continue
		}
		out[i] = Bytes{meta: meta{path: f.Path(), from: f.From()}, data: data}
	}
	return out, errors.Join(errs...)
}

func (fs *FS) addRendered(flist ...Bytes) error {
	var errs []error

	for _, f := range flist {
		if existing, has := fs.folded[strings.ToLower(f.path)]; has {
			rf := fs.m[existing]
			if existing == f.path {
				errs = append(errs, fmt.Errorf("cannot create %s for jenny %q, path already created by jenny %q", f.path, jennystack(f.from), stack(rf)))
			} else {
				errs = append(errs, fmt.Errorf("cannot create %s for jenny %q, path differs only by case from %s, created by jenny %q", f.path, jennystack(f.from), existing, stack(rf)))
			}
		}
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	for _, f := range flist {
		fs.m[f.path] = &fstest.MapFile{Data: f.data, Sys: f.from}
		fs.folded[strings.ToLower(f.path)] = f.path
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
	flist := fs2.contents()

	fs.mu.Lock()
	defer fs.mu.Unlock()

	return fs.addRendered(flist...)
}

// Len returns the number of items in the FS.
func (fs *FS) Len() int {
	fs.mu.Lock()
	ret := len(fs.m)
	fs.mu.Unlock()
	return ret
}

// FileMapper takes a File and transforms it into a new File. A FileMapper
// can't drop a file: returning a nil File is an error.
//
// [BytesMapper] and [TreeMapper] make FileMappers that work on a file's
// bytes, or on the value of a [Tree]. A FileMapper written directly can
// type-switch on the kind of File it's given, for example to work on a Tree
// when it has one and on bytes otherwise; [Move] changes a File's path
// without rendering it.
//
// Work on a Tree's value must come before work on its bytes, because a
// BytesMapper turns a Tree into Bytes.
//
// jenny generally assumes that FileMappers will reuse an
// unmodified byte slice.
type FileMapper func(File) (File, error)

// BytesMapper returns a [FileMapper] that passes the content of every file
// through fn. A [Tree] is rendered first, and replaced with [Bytes], so
// mappers after it see bytes.
func BytesMapper(fn func(f File, data []byte) ([]byte, error)) FileMapper {
	return func(f File) (File, error) {
		data, err := f.Render()
		if err != nil {
			return nil, err
		}
		data, err = fn(f, data)
		if err != nil {
			return nil, err
		}
		return Bytes{meta: meta{path: f.Path(), from: f.From()}, data: data}, nil
	}
}

// Map creates a new FS by passing each file in the receiver FS through the
// provided [FileMapper].
func (fs *FS) Map(fn FileMapper) (*FS, error) {
	nflist, err := mapFiles(fs.AsFiles(), fn)
	if err != nil {
		return nil, err
	}
	rendered, err := render(nflist)
	if err != nil {
		return nil, err
	}
	fs2 := NewFS()
	if err := fs2.addRendered(rendered...); err != nil {
		return nil, err
	}
	return fs2, nil
}

func toFile(path string, mf *fstest.MapFile) Bytes {
	return Bytes{meta: meta{path: path, from: mf.Sys.([]NamedJenny)}, data: mf.Data}
}

func stack(mf *fstest.MapFile) jennystack {
	return jennystack(mf.Sys.([]NamedJenny))
}
