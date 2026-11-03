package jenny

import "testing"

func TestFuncPackage(t *testing.T) {
	tests := map[string]string{
		"main.main":                            "main",
		"github.com/x/y.F":                     "github.com/x/y",
		"github.com/x/y/v2.(*T).M":             "github.com/x/y/v2",
		"github.com/x/y.F.func2":               "github.com/x/y",
		"github.com/x/y.Generic[...]":          "github.com/x/y",
		"github.com/x/y.Builder[...].New[...]": "github.com/x/y",
		// The documented limitation: a dot in the last path element.
		"gopkg.in/yaml.v3.F": "gopkg.in/yaml",
	}
	for fn, want := range tests {
		if got := funcPackage(fn); got != want {
			t.Errorf("funcPackage(%q) = %q, want %q", fn, got, want)
		}
	}
}
