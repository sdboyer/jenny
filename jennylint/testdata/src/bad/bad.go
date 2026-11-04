// Package bad has every kind of check jennylint reports.
package bad

import (
	"fmt"

	"github.com/sdboyer/jenny"
)

type ast struct{}

func pointerCases(f jenny.File) {
	switch f.(type) {
	case *jenny.Bytes: // want `case \*jenny.Bytes on a jenny.File never matches: jenny hands out a jenny.Bytes, never a pointer to one; use jenny.BytesOf`
	case *jenny.Tree[ast]: // want `case \*jenny.Tree\[ast\] on a jenny.File never matches: jenny hands out a jenny.Tree\[ast\], never a pointer to one; use jenny.TreeOf\[ast\]`
	}
	switch g := f.(type) {
	case *jenny.Tree[[]string], *jenny.Bytes: // want `case \*jenny.Tree\[\[\]string\]` `case \*jenny.Bytes`
		fmt.Println(g)
	}
}

func pointerAssertions(f jenny.File) {
	b, ok := f.(*jenny.Bytes) // want `type assertion to \*jenny.Bytes on a jenny.File never matches`
	fmt.Println(b, ok)
	fmt.Println(f.(*jenny.Tree[ast]).Value()) // want `type assertion to \*jenny.Tree\[ast\] on a jenny.File never matches: .*; use jenny.TreeOf\[ast\]`
}

func valueCases(f jenny.File) {
	switch f := f.(type) {
	case jenny.Bytes: // want `case jenny.Bytes on a jenny.File misses a \*jenny.Bytes; use jenny.BytesOf, which matches both`
		fmt.Println(f.Data())
	case jenny.Tree[ast]: // want `case jenny.Tree\[ast\] on a jenny.File misses a \*jenny.Tree\[ast\]; use jenny.TreeOf\[ast\], which matches both`
		fmt.Println(f.Value())
	default:
	}
}

func valueAssertions(fl jenny.Files) {
	if t, ok := fl[0].(jenny.Tree[ast]); ok { // want `type assertion to jenny.Tree\[ast\] on a jenny.File misses a \*jenny.Tree\[ast\]; use jenny.TreeOf\[ast\]`
		fmt.Println(t)
	}
	fmt.Println(fl[1].(jenny.Bytes).Data()) // want `type assertion to jenny.Bytes on a jenny.File misses`
}

func nils(f jenny.File, fl jenny.Files) bool {
	if f == nil { // want `comparing a jenny.File with nil misses a nil pointer such as a nil \*jenny.Bytes; use jenny.IsNil`
		return false
	}
	switch f.(type) {
	case nil: // want `case nil in a type switch on a jenny.File misses a nil pointer`
	}
	return nil != fl[0] && f != nil // want `comparing a jenny.File with nil` `comparing a jenny.File with nil`
}

// File is an alias of jenny.File, which is still checked.
type File = jenny.File

func alias(f File) bool {
	return f == nil // want `comparing a jenny.File with nil`
}
