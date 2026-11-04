// Package renamed imports jenny under another name. Messages still say
// jenny, and fixes use the file's name for it.
package renamed

import j "github.com/sdboyer/jenny"

func f(file j.File) bool {
	_, ok := file.(*j.Bytes) // want `type assertion to \*jenny.Bytes on a jenny.File never matches`
	return ok && file != nil // want `comparing a jenny.File with nil`
}
