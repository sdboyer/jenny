// Package nstest supports tests of how jenny attributes jennies to packages.
// It must be a package other than jenny's own and its tests'.
package nstest

import "github.com/sdboyer/jenny"

func file(s string) (*jenny.File, error) { return jenny.NewFile(s+".txt", nil), nil }

// Direct makes a jenny by calling New from this package.
func Direct(b jenny.Builder[string], name string) jenny.Jenny[string] {
	return b.New(name, file)
}

// ViaHelper makes a jenny through a helper, so it's attributed to the caller.
func ViaHelper(b jenny.Builder[string], name string) jenny.Jenny[string] {
	jenny.Helper()
	return b.New(name, file)
}
