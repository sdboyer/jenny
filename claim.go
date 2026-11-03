package jenny

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Claim is a File that, instead of content, claims ownership of every path
// matching a pattern. A jenny returns a Claim among its Files to say "the
// files here that match this pattern are mine". [FS.Verify] then reports as
// stale any file on disk that a Claim matches but that no jenny generated.
//
// The pattern is relative to wherever the jenny's files end up: decorators
// such as [Jenny.Prefix] place a Claim exactly as they place the jenny's other
// files. [FileMapper]s, which see only [Content], leave Claims alone.
//
// Patterns use [path.Match] syntax, which is slash-separated on every OS: '*'
// and '?' never match '/', so a Claim is not recursive. Matching ignores case,
// as path conflicts do (see [Files.Validate]).
//
// Claims may overlap, and a Claim may match files that other jennies
// generate: a file is stale only if no jenny generated it, whichever jenny's
// Claim matches it.
type Claim struct {
	// meta.path is the literal directory the glob is relative to, "." for
	// the root. Keeping it apart from the glob means placing a Claim never
	// has to escape the directory it's placed in.
	meta

	glob string
}

// NewClaim returns a [Claim] over the paths matching pattern, in [path.Match]
// syntax.
func NewClaim(pattern string, from ...NamedJenny) Claim {
	elems := strings.Split(pattern, "/")
	i := 0
	for i < len(elems)-1 && !hasMeta(elems[i]) {
		i++
	}
	dir := "."
	if i > 0 {
		dir = strings.Join(elems[:i], "/")
	}
	return Claim{meta: meta{path: dir, from: from}, glob: strings.Join(elems[i:], "/")}
}

func hasMeta(s string) bool {
	return strings.ContainsAny(s, `*?[\`)
}

// Path returns the Claim's pattern, with the directory it was placed in
// escaped, so it can be passed to [path.Match].
func (c Claim) Path() string {
	if c.path == "." {
		return c.glob
	}
	var b strings.Builder
	for _, r := range c.path {
		if strings.ContainsRune(`*?[\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String() + "/" + c.glob
}

// Match reports whether the slash-separated path p is in the Claim, ignoring
// case.
func (c Claim) Match(p string) bool {
	rel := strings.ToLower(p)
	if c.path != "." {
		var ok bool
		if rel, ok = strings.CutPrefix(rel, strings.ToLower(c.path)+"/"); !ok {
			return false
		}
	}
	ok, _ := path.Match(strings.ToLower(c.glob), rel)
	return ok
}

// depth is the number of path elements a matching path has beneath the
// Claim's directory.
func (c Claim) depth() int {
	return strings.Count(c.glob, "/") + 1
}

// Validate implements [File]. The directory a Claim is placed in follows the
// path rules of [Content]; its pattern must be a valid [path.Match] pattern,
// with no empty, '.' or '..' elements.
func (c Claim) Validate() error {
	if c.path != "." {
		if err := validatePath(c.path); err != nil {
			return fmt.Errorf("claim %s: %w", c.Path(), err)
		}
	}
	if c.glob == "" {
		return errors.New("claim patterns must not be empty")
	}
	for e := range strings.SplitSeq(c.glob, "/") {
		if e == "" || e == "." || e == ".." {
			return fmt.Errorf("claim %s: patterns must have no '.', '..' or empty elements", c.Path())
		}
	}
	if _, err := path.Match(c.glob, ""); err != nil {
		return fmt.Errorf("claim %s: %w", c.Path(), err)
	}
	if len(c.from) == 0 {
		return fmt.Errorf("claim %s: File must have at least one From jenny", c.Path())
	}
	return nil
}

func (c Claim) placed(dir string) File {
	c.meta = c.meta.in(dir)
	return c
}

func (c Claim) stamped(nj NamedJenny) File {
	c.meta = c.meta.by(nj)
	return c
}

// key identifies identical claims, which are redundant rather than
// conflicting.
func (c Claim) key() string {
	return c.Path() + "\x00" + c.FromString()
}
