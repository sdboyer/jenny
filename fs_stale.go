package jenny

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
)

// Stale returns the paths of the files in disk that a [Claim] in the FS
// matches, but that the FS has no file at, in path order. These are files a
// jenny says are its own, but that no jenny generated: typically left behind
// when an input went away.
//
// disk is usually [os.DirFS] of the directory the FS would be written to.
// Paths are slash-separated and relative to disk's root, as io/fs paths are on
// every OS. A file is generated if the FS has a file at its path, ignoring
// case, so that a file whose case differs on a case-insensitive filesystem is
// never reported.
//
// Stale reads disk only beneath each Claim's directory, and only as deep as
// its pattern reaches. A Claim whose directory doesn't exist matches nothing.
func (fsys *FS) Stale(ctx context.Context, disk fs.FS) ([]string, error) {
	found, err := fsys.stale(ctx, disk)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(found))
	for i, s := range found {
		out[i] = s.Path
	}
	return out, nil
}

func (fsys *FS) stale(ctx context.Context, disk fs.FS) ([]*StaleError, error) {
	fsys.mu.RLock()
	claims := make([]Claim, len(fsys.claims))
	copy(claims, fsys.claims)
	generated := make(map[string]bool, len(fsys.folded))
	for folded := range fsys.folded {
		generated[folded] = true
	}
	fsys.mu.RUnlock()

	var out []*StaleError
	seen := make(map[string]bool)
	for _, c := range claims {
		base, depth := c.path, c.depth()
		err := fs.WalkDir(disk, base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == base && errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if p != base && below(base, p) >= depth {
					return fs.SkipDir
				}
				return nil
			}
			if seen[p] || !c.Match(p) || generated[strings.ToLower(p)] {
				return nil
			}
			seen[p] = true
			out = append(out, &StaleError{Path: p, Claim: c})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// below returns the number of path elements p has beneath base, where p is
// beneath base, and both are slash-separated io/fs paths.
func below(base, p string) int {
	if base == "." {
		return strings.Count(p, "/") + 1
	}
	return strings.Count(p, "/") - strings.Count(base, "/")
}
