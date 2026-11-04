// Package noimport gets a jenny.File without importing jenny. Its nil check is
// reported, but has no fix, since IsNil would need an import.
package noimport

import "example.com/user/src/files"

func f() bool {
	return files.Get() != nil // want `comparing a jenny.File with nil`
}
