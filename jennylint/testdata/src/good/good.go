// Package good asks what Files hold the right way, and does other things
// that look similar but aren't about a jenny.File. jennylint reports nothing.
package good

import (
	"errors"
	"fmt"

	"github.com/sdboyer/jenny"
)

type ast struct{}

func helpers(f jenny.File) {
	if jenny.IsNil(f) {
		return
	}
	if v, ok := jenny.TreeOf[ast](f); ok {
		fmt.Println(v)
	} else if data, ok := jenny.BytesOf(f); ok {
		fmt.Println(data)
	}
}

// A concrete pointer's nil check is exact.
func concrete(b *jenny.Bytes, t *jenny.Tree[ast]) bool {
	return b == nil || t != nil
}

// Neither the static type of x nor the comparison is a jenny.File.
func other(x any, err error, f jenny.File) {
	switch x.(type) {
	case *jenny.Bytes, jenny.Tree[ast], nil:
	}
	if _, ok := x.(jenny.Bytes); ok {
		fmt.Println(x == nil, err != nil)
	}
	// Converting to any first compares the interface exactly, for code that
	// means it.
	fmt.Println(any(f) == nil)
	// Asserting to another interface asks nothing about jenny's kinds.
	if s, ok := f.(fmt.Stringer); ok {
		fmt.Println(s)
	}
	if errors.Is(err, nil) {
		fmt.Println(f.Path())
	}
}
