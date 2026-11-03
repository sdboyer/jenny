package jenny

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// File is one output of a jenny: either content at a path, which is a
// [Content] ([Bytes] or [Tree]), or a [Claim] of ownership over the paths
// matching a pattern.
//
// File is sealed: only this package's types implement it. That lets the
// framework place ([Jenny.Prefix]) and stamp ([Builder.New]) any File without
// knowing what's in it. [Tree] is generic, so content can still be carried in
// any representation.
type File interface {
	// Path is the slash-separated path of the File, relative to the output
	// root. For a Claim, it's the claim's pattern.
	Path() string

	// From is the stack of named jennies responsible for producing the File,
	// outermost first. Jennies made with [Builder.New] add themselves to it.
	From() []NamedJenny

	// FromString joins the names of the jennies in From with colons.
	FromString() string

	// Validate checks that the File's path (or a Claim's pattern) is clean,
	// relative and slash-separated, and that the File has at least one jenny
	// in its From.
	Validate() error

	// placed returns a copy of the File beneath dir.
	placed(dir string) File

	// stamped returns a copy of the File with nj outermost in its From.
	stamped(nj NamedJenny) File
}

// Content is a File with content, which Render returns as bytes. [Bytes] and
// [Tree] are Contents; a [Claim] is not.
type Content interface {
	File

	// Render returns the File's content as bytes.
	Render() ([]byte, error)
}

// meta is the path and provenance every File has.
type meta struct {
	path string
	from []NamedJenny
}

func (m meta) Path() string { return m.path }

func (m meta) From() []NamedJenny { return m.from }

func (m meta) FromString() string { return jennystack(m.from).String() }

// Validate checks that a Content's path is a clean, relative,
// slash-separated path (per [io/fs.ValidPath]) that means the same thing on
// every OS, and that it has at least one jenny in its From.
//
// A path that means the same thing on every OS has no backslashes, no colons
// (drive letters or stream names on Windows), no NUL bytes, and no element
// that Windows reserves as a device name, such as NUL, COM1 or aux.go. These
// rules don't depend on the OS Validate runs on, so a path is rejected
// wherever it is generated, not only when it is written on Windows.
//
// The path rules guarantee that writing the File beneath a directory, as
// [FS.Write] does, can never place it outside that directory, and that the
// File is reachable through the [FS]'s [io/fs.FS] implementation.
func (m meta) Validate() error {
	if m.path == "" {
		return errors.New("File paths must not be empty")
	}
	if err := checkPath(m.path); err != nil {
		return err
	}
	return m.validateFrom()
}

func (m meta) validateFrom() error {
	if len(m.from) == 0 {
		return fmt.Errorf("%s: File must have at least one From jenny", m.path)
	}
	return nil
}

func (m meta) in(dir string) meta {
	m.path = path.Join(dir, m.path)
	return m
}

func (m meta) by(nj NamedJenny) meta {
	m.from = append([]NamedJenny{nj}, m.from...)
	return m
}

// NewFile returns a [Bytes] file at path with the given data.
func NewFile(path string, data []byte, from ...NamedJenny) Bytes {
	return Bytes{meta: meta{path: path, from: from}, data: data}
}

// Bytes is a File whose content is a byte slice. Make one with [NewFile].
type Bytes struct {
	meta

	data []byte
}

// Data returns the file's contents.
func (b Bytes) Data() []byte { return b.data }

// Render implements [Content]. It returns Data, and never fails.
func (b Bytes) Render() ([]byte, error) { return b.data, nil }

func (b Bytes) placed(dir string) File {
	b.meta = b.meta.in(dir)
	return b
}

func (b Bytes) stamped(nj NamedJenny) File {
	b.meta = b.meta.by(nj)
	return b
}

// Files is a set of File objects.
//
// A Files is invalid (per [Files.Validate]) if it contains a nil File, a File
// that doesn't [File.Validate], or more than one [Content] having the same
// [File.Path] (note: this is case-insensitive). Claims may repeat and
// overlap.
//
// These invariants are internally enforced by FS.
type Files []File

// Validate checks every File with [File.Validate], and checks that no two
// Contents have paths that are equal, ignoring case. All problems found are
// returned together.
func (fsl Files) Validate() error {
	var errs []error

	paths := make(map[string][][]NamedJenny)
	for _, f := range fsl {
		if f == nil {
			errs = append(errs, errors.New("nil File not allowed within Files slice"))
			continue
		}
		if err := f.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, ok := f.(Claim); ok {
			// Claims own regions, not paths: they may repeat (a single-input
			// jenny returns its claim once per input) and overlap.
			continue
		}
		normalizedPath := strings.ToLower(f.Path())
		paths[normalizedPath] = append(paths[normalizedPath], f.From())
	}
	for path, froms := range paths {
		if len(froms) > 1 {
			fstr := make([]string, 0, len(froms))
			for _, from := range froms {
				fstr = append(fstr, "'"+jennystack(from).String()+"'")
			}
			errs = append(errs, fmt.Errorf("multiple files at path %s from jennies: %s", path, strings.Join(fstr, ", ")))
		}
	}

	return errors.Join(errs...)
}
