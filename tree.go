package jenny

import (
	"errors"
	"fmt"
	"reflect"
)

// Tree is a File whose content is a value of type T, such as a syntax tree,
// which a render func turns into bytes.
//
// A Tree lets [FileMapper]s work on the value rather than on bytes (see
// [TreeMapper]). It holds only the value, never a rendered copy that could go
// stale. It's rendered only when bytes are needed: when a [BytesMapper] maps
// it, when it's added to an [FS], or when Render is called.
//
// T can be any value that render turns into bytes: a tree from go/ast
// rendered by go/format, one from CUE's or another language's AST package
// rendered by its printer, or a type of your own. jenny never parses,
// formats or inspects the value; formatting is whatever render does.
//
// Work on the tree must come before work on the bytes. A BytesMapper turns a
// Tree into [Bytes], so a TreeMapper for it after the BytesMapper reports an
// error rather than finding no Tree.
type Tree[T any] struct {
	meta

	value  T
	render func(T) ([]byte, error)
}

// NewTree returns a [Tree] at path, holding value, that render turns into
// bytes.
func NewTree[T any](path string, value T, render func(T) ([]byte, error), from ...NamedJenny) Tree[T] {
	return Tree[T]{meta: meta{path: path, from: from}, value: value, render: render}
}

// Value returns the value the Tree holds.
func (t Tree[T]) Value() T { return t.value }

// Render implements [File] by calling the Tree's render func on its value.
func (t Tree[T]) Render() ([]byte, error) { return t.render(t.value) }

// Validate implements [File], additionally requiring a render func.
func (t Tree[T]) Validate() error {
	if err := t.meta.Validate(); err != nil {
		return err
	}
	if t.render == nil {
		return errors.New(t.path + ": Tree must have a render func")
	}
	return nil
}

func (t Tree[T]) withPath(fn func(string) string) File {
	t.meta = t.meta.withPath(fn)
	return t
}

func (t Tree[T]) stamped(nj NamedJenny) File {
	t.meta = t.meta.by(nj)
	return t
}

// TreeMapper returns a [FileMapper] that passes the value of every Tree[T]
// through fn, and makes a Tree of the value fn returns. Every other File,
// including a Tree of a different type, passes through unchanged.
//
// A file that a [BytesMapper] rendered from a Tree[T] is an error: the tree
// work was meant for it, but ran after byte work had already turned it into
// [Bytes]. Put TreeMappers before BytesMappers.
//
// fn must return a new value rather than change the one it's given in place.
// The value isn't copied as a Tree passes through jennies, so if T is or holds
// a pointer, as most AST packages' node types do, a change in place also
// changes every other File holding that value, such as one a generator func
// cached, or the input to an earlier Map.
func TreeMapper[T any](fn func(f File, value T) (T, error)) FileMapper {
	return func(f File) (File, error) {
		f = normalize(f)
		if f == nil {
			return nil, errors.New("TreeMapper given a nil File")
		}
		t, ok := f.(Tree[T])
		if !ok {
			if b, ok := f.(Bytes); ok && b.renderedFrom == reflect.TypeFor[Tree[T]]() {
				return nil, fmt.Errorf("%s was rendered from a %v by a BytesMapper before this TreeMapper ran: tree mappers must come before byte mappers", f.Path(), b.renderedFrom)
			}
			return f, nil
		}
		v, err := fn(f, t.value)
		if err != nil {
			return nil, err
		}
		t.value = v
		return t, nil
	}
}
