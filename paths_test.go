package jenny_test

import (
	"path/filepath"
	"testing"

	"github.com/sdboyer/jenny"
)

// pathCases are File paths and whether File.Validate accepts them. Validate
// uses slash semantics on every OS, so each case has the same answer
// everywhere; the inputs that path/filepath reads differently on Windows are
// written out explicitly rather than built with filepath.
var pathCases = map[string]struct {
	path  string
	valid bool
}{
	"plain":                     {"a.go", true},
	"nested":                    {"a/b/c.go", true},
	"absolute":                  {"/x", false},
	"parent":                    {"../x", false},
	"inner parent":              {"a/../../x", false},
	"dot":                       {"./x", false},
	"empty element":             {"a//b", false},
	"trailing slash":            {"a/", false},
	"backslash separator":       {`a\b`, false},
	"backslash escape":          {`a\..\..\b`, false},
	"UNC":                       {`\\host\share\x`, false},
	"drive absolute":            {"C:/x", false},
	"drive relative":            {"C:x", false},
	"drive only":                {"C:", false},
	"stream name":               {"a.go:stream", false},
	"NUL byte":                  {"a\x00b", false},
	"device":                    {"nul", false},
	"device upper":              {"CON", false},
	"device in dir":             {"pkg/aux/x.go", false},
	"device with extension":     {"aux.go", false},
	"device with two exts":      {"prn.tar.gz", false},
	"device trailing space":     {"con .txt", false},
	"COM port":                  {"com1", false},
	"LPT port with extension":   {"LPT9.txt", false},
	"superscript port":          {"COM\u00b9", false},
	"console input":             {"CONIN$", false},
	"console output":            {"conout$.log", false},
	"device prefix":             {"console.go", true},
	"device suffix":             {"auxiliary/x.go", true},
	"device in name":            {"nul_test.go", true},
	"COM port zero":             {"com0", true},
	"COM port two digits":       {"com10", true},
	"dotfile":                   {".gitignore", true},
	"device as later extension": {"x.nul", true},
}

func TestFileValidatePaths(t *testing.T) {
	for name, tt := range pathCases {
		t.Run(name, func(t *testing.T) {
			err := jenny.NewFile(tt.path, nil, src("t")).Validate()
			if (err == nil) != tt.valid {
				t.Errorf("Validate(%q) = %v, want valid %v", tt.path, err, tt.valid)
			}
		})
	}
}

// Every path Validate accepts must be writable on the OS running the test, and
// stay beneath the directory it's written to. Validate doesn't depend on the
// OS, so on Windows this checks its rules cover everything Windows rejects.
func TestValidPathsLocalize(t *testing.T) {
	for name, tt := range pathCases {
		if !tt.valid {
			continue
		}
		t.Run(name, func(t *testing.T) {
			local, err := filepath.Localize(tt.path)
			if err != nil {
				t.Fatalf("Validate accepts %q, but Localize rejects it: %v", tt.path, err)
			}
			if !filepath.IsLocal(local) {
				t.Errorf("Validate accepts %q, but it localizes to non-local %q", tt.path, local)
			}
		})
	}
}

// Prefix joins with slash semantics, so a prefix that only means a directory
// on some OSes is caught on every OS.
func TestPrefixRejectsNonPortableDir(t *testing.T) {
	j := jenny.For[string]().New("J", func(s string) (jenny.File, error) {
		return jenny.NewFile(s, nil), nil
	})
	for _, dir := range []string{`pkg\api`, "C:", "C:/pkg", "/abs", "../up", "nul"} {
		if _, err := j.Prefix(dir).Generate("x.go"); err == nil {
			t.Errorf("Prefix(%q) produced a valid path", dir)
		}
	}
	fl, err := j.Prefix("pkg/api").Generate("x.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := fl[0].Path(); got != "pkg/api/x.go" {
		t.Errorf("Prefix(\"pkg/api\") gave %q, want pkg/api/x.go", got)
	}
}
