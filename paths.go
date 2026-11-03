package jenny

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// checkPath enforces the path rules of [File.Validate]. A File's path is an
// [io/fs] name, slash-separated on every OS, so it is checked with
// [io/fs.ValidPath] and package path, never path/filepath, whose answers
// depend on the OS running the check.
//
// The rules reject everything [path/filepath.Localize] rejects on any OS, so
// that a path valid on one OS can be written on all of them, and a generator
// can't produce output that fails only on Windows.
func checkPath(p string) error {
	if path.IsAbs(p) {
		return fmt.Errorf("%s: File paths must be relative", p)
	}
	if !fs.ValidPath(p) || strings.Contains(p, `\`) {
		return fmt.Errorf("%s: File paths must be clean and slash-separated, with no '.', '..' or empty elements and no backslashes", p)
	}
	if strings.ContainsAny(p, ":\x00") {
		return fmt.Errorf("%s: File paths must not contain a colon, which Windows reads as a drive letter or stream name, or a NUL byte", p)
	}
	for elem := range strings.SplitSeq(p, "/") {
		if windowsDevice(elem) {
			return fmt.Errorf("%s: File paths must not use %q, which Windows reserves as a device name", p, elem)
		}
	}
	return nil
}

// windowsDevice reports whether a path element names a Windows device, such
// as NUL or COM1, which Windows opens in place of a file in any directory.
//
// Whether a device name followed by an extension (nul.txt) is also reserved
// depends on the Windows version, so such names are always rejected, as
// golang.org/x/mod/module.CheckFilePath does for module zips. The set and
// matching otherwise follow Go's own check, which [path/filepath.Localize]
// applies on Windows.
func windowsDevice(elem string) bool {
	base, _, _ := strings.Cut(elem, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		switch base[3:] {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
			return true
		}
	}
	return false
}
