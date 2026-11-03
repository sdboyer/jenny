package jenny

import "errors"

// Tree is a File whose content is a value of type T, such as a syntax tree,
// which a render func turns into bytes.
//
// A Tree lets [FileMapper]s work on the value rather than on bytes (see
// [TreeMapper]). It's rendered only when bytes are needed: when a
// [BytesMapper] maps it, when it's added to an [FS], or when Render is called.
//
// The value is not copied as the Tree passes through jennies. If T is a
// pointer, or holds one, as an *ast.File does, a TreeMapper that changes it in
// place changes it for every holder of that value.
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

// Render implements [Content] by calling the Tree's render func on its value.
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

func (t Tree[T]) placed(dir string) File {
	t.meta = t.meta.in(dir)
	return t
}

func (t Tree[T]) stamped(nj NamedJenny) File {
	t.meta = t.meta.by(nj)
	return t
}

// TreeMapper returns a [FileMapper] that passes the value of every Tree[T]
// through fn. Every other File, including a Tree of a different type or one
// already rendered to [Bytes], passes through unchanged.
func TreeMapper[T any](fn func(f File, value T) (T, error)) FileMapper {
	return func(f Content) (Content, error) {
		t, ok := f.(Tree[T])
		if !ok {
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
