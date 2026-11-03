package jenny

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
// FS implements [io/fs.FS], backed by a copy of [testing/fstest.MapFS]. Added
// mutexes make FS safe for concurrent use, but it has the same scaling
// limitations as [testing/fstest.MapFS] for large numbers of files.
//
// FS holds [Content] rendered to bytes: a [Tree] is rendered when it's added.
// It also holds the [Claim]s added to it, which are not files, and so are
// invisible through its io/fs methods. FS is stateless, so it can't know that
// a file on disk that nothing generated this run was once generated, unless a
// Claim says so (see [FS.Stale]).
type FS struct {
	m fstest.MapFS

	// folded maps the lowercased form of each path in m to the path
	// itself, so conflicts can be detected case-insensitively, consistent
	// with [Files.Validate].
	folded map[string]string

	// claims holds one of each distinct Claim added, in the order added.
	claims []Claim

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
// order, followed by every stale file (see [FS.Stale]), in path order.
//
// Verify stops early, returning an error wrapping ctx's error, if ctx is done.
//
// If the provided prefix path is non-empty, it will be prepended to all file
// entries in the map for writing. prefix may be an absolute path.
func (fs *FS) Verify(ctx context.Context, prefix string) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(12)

	files := fs.contents()
	// Indexed by file, so the joined error lists files in path order no matter
	// which goroutine finishes first.
	diffs := make([]error, len(files))

	for i, item := range files {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			ipath := filepath.Join(prefix, filepath.FromSlash(item.Path()))
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
			dstr := cmp.Diff(string(ob), string(item.data))
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

	root := prefix
	if root == "" {
		root = "."
	}
	stale, err := fs.stale(ctx, os.DirFS(root))
	if err != nil {
		return fmt.Errorf("io error while verifying tree: %w", err)
	}
	staleErrs := make([]error, 0, len(stale))
	for _, s := range stale {
		staleErrs = append(staleErrs, fmt.Errorf("%s: stale: matches claim %s of jenny %q, but was not generated",
			filepath.Join(prefix, filepath.FromSlash(s.path)), s.claim.Path(), jennystack(s.claim.from)))
	}

	return errors.Join(errors.Join(diffs...), errors.Join(staleErrs...))
}

// Write writes all of the files to their indicated paths. It never removes
// files, including stale ones (see [FS.Stale]).
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
			path := filepath.Join(prefix, filepath.FromSlash(it.Path()))
			err := os.MkdirAll(filepath.Dir(path), os.ModePerm)
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

// AsFiles returns a Files representing the contents of the FS: a [Bytes] for
// each file, and each distinct [Claim].
//
// The contents are sorted lexicographically by [File.Path], and it is
// guaranteed that the invariants enforced by [Files.Validate] are met.
func (fs *FS) AsFiles() []File {
	if fs == nil {
		return nil
	}

	fs.mu.RLock()
	sl := make([]File, 0, len(fs.m)+len(fs.claims))
	for path, fi := range fs.m {
		sl = append(sl, toFile(path, fi))
	}
	for _, c := range fs.claims {
		sl = append(sl, c)
	}
	fs.mu.RUnlock()

	sort.SliceStable(sl, func(i, j int) bool {
		return sl[i].Path() < sl[j].Path()
	})
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

// Add adds one or more files to the FS, rendering each [Content]. An error is
// returned if the path of any provided Content already exists in the FS, or
// if one fails to render. Adding a Claim identical to one already in the FS
// (the same pattern, from the same jennies) has no effect.
func (fs *FS) Add(flist ...File) error {
	prepared, err := prepare(flist)
	if err != nil {
		return err
	}
	fs.mu.Lock()
	err = fs.addPrepared(prepared...)
	fs.mu.Unlock()
	return err
}

// prepare validates flist and renders every Content in it to Bytes.
func prepare(flist []File) ([]File, error) {
	if err := Files(flist).Validate(); err != nil {
		return nil, err
	}

	var errs []error
	out := make([]File, len(flist))
	for i, f := range flist {
		switch f := f.(type) {
		case Claim, Bytes:
			out[i] = f
		case Content:
			data, err := f.Render()
			if err != nil {
				errs = append(errs, fmt.Errorf("rendering %s from %q failed: %w", f.Path(), jennystack(f.From()), err))
				continue
			}
			out[i] = Bytes{meta: meta{path: f.Path(), from: f.From()}, data: data}
		}
	}
	return out, errors.Join(errs...)
}

func (fs *FS) addPrepared(flist ...File) error {
	var errs []error

	for _, f := range flist {
		if _, ok := f.(Claim); ok {
			continue
		}
		if existing, has := fs.folded[strings.ToLower(f.Path())]; has {
			rf := fs.m[existing]
			if existing == f.Path() {
				errs = append(errs, fmt.Errorf("cannot create %s for jenny %q, path already created by jenny %q", f.Path(), jennystack(f.From()), stack(rf)))
			} else {
				errs = append(errs, fmt.Errorf("cannot create %s for jenny %q, path differs only by case from %s, created by jenny %q", f.Path(), jennystack(f.From()), existing, stack(rf)))
			}
		} else if filepath.IsAbs(f.Path()) {
			errs = append(errs, fmt.Errorf("files must have relative paths, got %s from %q", f.Path(), jennystack(f.From())))
		}
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	for _, f := range flist {
		switch f := f.(type) {
		case Claim:
			if !slices.ContainsFunc(fs.claims, func(c Claim) bool { return c.key() == f.key() }) {
				fs.claims = append(fs.claims, f)
			}
		case Bytes:
			fs.m[f.path] = &fstest.MapFile{Data: f.data, Sys: f.from}
			fs.folded[strings.ToLower(f.path)] = f.path
		}
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
	flist, err := prepare(fs2.AsFiles())
	if err != nil {
		return err
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	return fs.addPrepared(flist...)
}

// Len returns the number of files in the FS. Claims are not counted.
func (fs *FS) Len() int {
	fs.mu.Lock()
	ret := len(fs.m)
	fs.mu.Unlock()
	return ret
}

// FileMapper takes a [Content] and transforms it into a new Content. Claims
// are never passed to a FileMapper; only [Jenny.Prefix] places them.
//
// [BytesMapper] and [TreeMapper] make FileMappers that work on a file's
// bytes, or on the value of a [Tree].
//
// jenny generally assumes that FileMappers will reuse an
// unmodified byte slice.
type FileMapper func(Content) (Content, error)

// BytesMapper returns a [FileMapper] that passes the content of every file
// through fn. A [Tree] is rendered first, and replaced with [Bytes], so
// mappers after it see bytes.
func BytesMapper(fn func(f File, data []byte) ([]byte, error)) FileMapper {
	return func(f Content) (Content, error) {
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
// provided [FileMapper]. Claims are carried over unchanged.
func (fs *FS) Map(fn FileMapper) (*FS, error) {
	nflist, err := mapContent(fs.AsFiles(), fn)
	if err != nil {
		return nil, err
	}
	prepared, err := prepare(nflist)
	if err != nil {
		return nil, err
	}
	fs2 := NewFS()
	if err := fs2.addPrepared(prepared...); err != nil {
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
