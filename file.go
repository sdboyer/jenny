package jenny

import (
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"
)

// File is a single file, intended to be written or compared against existing
// files on disk through an [FS]. Its content is held in one of two kinds:
// [Bytes], made with [NewFile], or [Tree], a value such as a syntax tree that
// is rendered to bytes only when they're needed.
//
// File is sealed: only this package's types implement it. That lets the
// framework move ([Jenny.Prefix], [Move]) and stamp ([Builder.New]) any File
// and keep its kind, so a [TreeMapper] still finds its Tree afterwards. It's
// also what lets the framework guarantee that a nil pointer is never mistaken
// for a File: wherever user code hands Files to jenny, a nil *Bytes or
// *Tree[T] is treated exactly as a nil File.
type File interface {
	// Path is the slash-separated path of the File, relative to the output
	// root.
	Path() string

	// From is the stack of named jennies responsible for producing the File,
	// outermost first. Jennies made with [Builder.New] add themselves to it.
	From() []NamedJenny

	// FromString joins the names of the jennies in From with colons.
	FromString() string

	// Render returns the File's content as bytes.
	Render() ([]byte, error)

	// Validate checks that the File's path is valid and that it has at least
	// one jenny in its From. A kind may add rules of its own.
	Validate() error

	// withPath returns a copy of the File with its path passed through fn.
	withPath(fn func(string) string) File

	// stamped returns a copy of the File with nj outermost in its From.
	stamped(nj NamedJenny) File
}

// normalize returns f as jenny holds Files: nil if f is nil or a nil pointer,
// and the value a pointer points to otherwise, so that a nil *Bytes is never
// mistaken for a File, and a *Tree[T] is still found by a TreeMapper. It is
// applied wherever user code hands Files to jenny.
func normalize(f File) File {
	if f == nil {
		return nil
	}
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Pointer {
		return f
	}
	if v.IsNil() {
		return nil
	}
	// Every kind implements File with value receivers, so the value a
	// pointer points to is a File too.
	return v.Elem().Interface().(File)
}

// Move returns a copy of f at path p, with its content, kind and From
// unchanged. Unlike rebuilding the file with [NewFile], it doesn't render a
// [Tree]. It returns nil if f is nil.
func Move(f File, p string) File {
	f = normalize(f)
	if f == nil {
		return nil
	}
	return f.withPath(func(string) string { return p })
}

// meta is the path and provenance every File has.
type meta struct {
	path string
	from []NamedJenny
}

func (m meta) Path() string { return m.path }

func (m meta) From() []NamedJenny { return m.from }

func (m meta) FromString() string { return jennystack(m.from).String() }

// Validate checks that the File's path is a clean, relative,
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
	if len(m.from) == 0 {
		return fmt.Errorf("%s: File must have at least one From jenny", m.path)
	}
	return nil
}

func (m meta) withPath(fn func(string) string) meta {
	m.path = fn(m.path)
	return m
}

func (m meta) by(nj NamedJenny) meta {
	m.from = append([]NamedJenny{nj}, m.from...)
	return m
}

// prefixer returns a func that places a path beneath dir.
func prefixer(dir string) func(string) string {
	return func(p string) string { return path.Join(dir, p) }
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

// Render implements [File]. It returns Data, and never fails.
func (b Bytes) Render() ([]byte, error) { return b.data, nil }

func (b Bytes) withPath(fn func(string) string) File {
	b.meta = b.meta.withPath(fn)
	return b
}

func (b Bytes) stamped(nj NamedJenny) File {
	b.meta = b.meta.by(nj)
	return b
}

// Files is a set of File objects.
//
// A Files is invalid (per [Files.Validate]) if it contains a nil File
// (including a nil pointer), a File that doesn't [File.Validate], or more
// than one File having the same [File.Path] (note: this is case-insensitive).
//
// These invariants are internally enforced by FS.
type Files []File

// Validate checks every File with [File.Validate], and checks that no two
// Files have paths that are equal, ignoring case. All problems found are
// returned together.
func (fsl Files) Validate() error {
	var errs []error

	paths := make(map[string][][]NamedJenny)
	for _, f := range fsl {
		if normalize(f) == nil {
			errs = append(errs, errors.New("nil File not allowed within Files slice"))
			continue
		}
		if err := f.Validate(); err != nil {
			errs = append(errs, err)
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
