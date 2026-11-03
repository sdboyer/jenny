package jenny

import (
	"fmt"
	"path"
	"strings"
)

// Claim says that every file matching a pattern belongs to a jenny: files
// there that the jenny's output doesn't include are stale, typically left
// behind when an input went away. A jenny declares its claims with [Owns]
// when it's made, and [FS.Verify] and [FS.Stale] report stale files.
//
// A claim's pattern is relative to wherever the jenny's files end up:
// [Jenny.Prefix] places a jenny's claims exactly as it places its files.
// Claims are part of a jenny, not of what it generates, so a jenny claims its
// files even in a run where it gets no inputs and generates nothing.
//
// Patterns use [path.Match] syntax, which is slash-separated on every OS: '*'
// and '?' never match '/', so a claim is not recursive. Matching ignores case,
// as path conflicts do (see [Files.Validate]).
//
// Claims may overlap, and a claim may match files that other jennies
// generate: a file is stale only if no jenny generated it, whichever jenny's
// claim matches it.
type Claim struct {
	// meta.path is the literal directory the glob is relative to, "." for
	// the root. Keeping it apart from the glob means placing a claim never
	// has to escape the directory it's placed in.
	meta

	glob string
}

func newClaim(pattern string, from ...NamedJenny) Claim {
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

// Path returns the claim's pattern, with the directory it was placed in
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

// Match reports whether the slash-separated path p is in the claim, ignoring
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
// claim's directory.
func (c Claim) depth() int {
	return strings.Count(c.glob, "/") + 1
}

// validate checks that the directory a claim is placed in follows the path
// rules of [File.Validate], and that its pattern is a valid [path.Match]
// pattern with no empty, '.' or '..' elements.
func (c Claim) validate() error {
	if c.path != "." {
		if err := checkPath(c.path); err != nil {
			return fmt.Errorf("claim %s of jenny %q: %w", c.Path(), c.FromString(), err)
		}
	}
	if c.glob == "" {
		return fmt.Errorf("claim of jenny %q: patterns must not be empty", c.FromString())
	}
	for e := range strings.SplitSeq(c.glob, "/") {
		if e == "" || e == "." || e == ".." {
			return fmt.Errorf("claim %s of jenny %q: patterns must have no '.', '..' or empty elements", c.Path(), c.FromString())
		}
	}
	if _, err := path.Match(c.glob, ""); err != nil {
		return fmt.Errorf("claim %s of jenny %q: %w", c.Path(), c.FromString(), err)
	}
	return nil
}

func (c Claim) placed(dir string) Claim {
	c.meta = c.meta.withPath(prefixer(dir))
	return c
}

// key identifies identical claims, which are redundant rather than
// conflicting.
func (c Claim) key() string {
	return c.Path() + "\x00" + c.FromString()
}

// NewOption configures a jenny made with [Builder.New].
type NewOption func(*newConfig)

type newConfig struct {
	owns []string
}

// Owns declares that the jenny owns every file matching each pattern, in
// [path.Match] syntax, relative to the jenny's own output. See [Claim].
func Owns(patterns ...string) NewOption {
	return func(c *newConfig) {
		c.owns = append(c.owns, patterns...)
	}
}

// StaleError reports a file that a [Claim] matches but that no jenny
// generated, as found by [FS.VerifyFS] and [FS.Verify].
//
// It is a sibling of [DriftError] rather than a kind of it. A DriftError is
// about a generated file and its persisted copy, and its fields describe how
// that copy is missing or differs; a stale file is the reverse, a persisted
// file with no generated counterpart.
type StaleError struct {
	// Path is the stale file's name in the persisted [io/fs.FS]:
	// slash-separated on every OS.
	Path string

	// Claim is the claim that matches the file. If several do, it is the
	// first, in the order the FS holds them.
	Claim Claim
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("%s: stale: matches claim %s of jenny %q, but was not generated", e.Path, e.Claim.Path(), e.Claim.FromString())
}
