package jenny

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
)

// DriftError reports one generated file whose persisted copy is missing or
// differs from it, as found by [FS.VerifyFS] and [FS.Verify].
type DriftError struct {
	// Path is the generated File's RelativePath, which is also its name in
	// the persisted [io/fs.FS]: slash-separated on every OS.
	Path string

	// Missing is true if the persisted copy does not exist. Diff is then
	// empty.
	Missing bool

	// Diff is the difference between the persisted copy and the generated
	// file, as reported by [github.com/google/go-cmp/cmp.Diff], with the
	// persisted contents as "-" and the generated contents as "+".
	Diff string
}

func (e *DriftError) Error() string {
	if e.Missing {
		return e.Path + ": generated file should exist, but does not"
	}
	return fmt.Sprintf("%s would have changed:\n\n%s", e.Path, e.Diff)
}

// VerifyFS checks the contents of each file against its persisted copy, the
// file of the same name in persisted. persisted may be any [io/fs.FS], such as
// a directory through [os.DirFS], an [embed.FS] or a [testing/fstest.MapFS].
// Files in persisted that the FS doesn't contain are ignored, unless a
// [Claim] matches them (see [FS.Stale]).
//
// If any files are missing or differ, VerifyFS returns a [*DriftError] for
// each, combined with [errors.Join] in path order and followed by an error
// for each stale file; use [errors.As] to find them. If reading persisted fails other than by a file not existing,
// VerifyFS returns that error instead, and no DriftErrors.
//
// VerifyFS reads persisted from several goroutines at once. It stops early,
// returning an error wrapping ctx's error, if ctx is done.
func (fsys *FS) VerifyFS(ctx context.Context, persisted fs.FS) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(12)

	files := fsys.contents()
	// Indexed by file, so the joined error lists files in path order no matter
	// which goroutine finishes first.
	diffs := make([]error, len(files))

	for i, item := range files {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			ob, err := fs.ReadFile(persisted, item.path)
			if errors.Is(err, fs.ErrNotExist) {
				diffs[i] = &DriftError{Path: item.path, Missing: true}
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s: error reading file: %w", item.path, err)
			}
			if dstr := cmp.Diff(string(ob), string(item.data)); dstr != "" {
				diffs[i] = &DriftError{Path: item.path, Diff: dstr}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("io error while verifying tree: %w", err)
	}

	stale, err := fsys.stale(ctx, persisted)
	if err != nil {
		return fmt.Errorf("io error while verifying tree: %w", err)
	}
	all := make([]error, 0, len(diffs)+len(stale))
	all = append(all, diffs...)
	for _, s := range stale {
		all = append(all, fmt.Errorf("%s: stale: matches claim %s of jenny %q, but was not generated",
			s.path, s.claim.Path(), jennystack(s.claim.from)))
	}

	return errors.Join(all...)
}
