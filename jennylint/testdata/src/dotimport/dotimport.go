// Package dotimport dot-imports jenny, so fixes call IsNil unqualified.
package dotimport

import . "github.com/sdboyer/jenny"

func f(file File) bool {
	return file == nil // want `comparing a jenny.File with nil`
}
