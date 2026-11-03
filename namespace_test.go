package jenny_test

import (
	"strings"
	"testing"

	"github.com/sdboyer/jenny"
	"github.com/sdboyer/jenny/internal/nstest"
)

const (
	thisPkg  = "github.com/sdboyer/jenny_test"
	otherPkg = "github.com/sdboyer/jenny/internal/nstest"
)

func pkgOf(t *testing.T, j jenny.Jenny[string]) string {
	t.Helper()
	fl := mustGen(t, j, "a")
	return fl[0].From[0].JennyPackage()
}

func TestJennyPackage(t *testing.T) {
	str := jenny.For[string]()
	file := func(s string) (*jenny.File, error) { return jenny.NewFile(s+".txt", nil), nil }
	inClosure := func() jenny.Jenny[string] { return str.New("Closure", file) }

	tests := map[string]struct {
		j    jenny.Jenny[string]
		want string
	}{
		"direct":                     {str.New("Direct", file), thisPkg},
		"in a closure":               {inClosure(), thisPkg},
		"other package":              {nstest.Direct(str, "Other"), otherPkg},
		"through a helper":           {nstest.ViaHelper(str, "Helped"), thisPkg},
		"namespace":                  {str.Namespace("gopkg.in/yaml.v3").New("NS", file), "gopkg.in/yaml.v3"},
		"namespace in other package": {nstest.Direct(str.Namespace("example.com/ns"), "NS"), "example.com/ns"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := pkgOf(t, tt.j); got != tt.want {
				t.Errorf("JennyPackage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNameRecordsCaller(t *testing.T) {
	if got := jenny.Name("x").JennyPackage(); got != thisPkg {
		t.Errorf("JennyPackage() = %q, want %q", got, thisPkg)
	}
}

func TestFromStringStaysShort(t *testing.T) {
	str := jenny.For[string]()
	fl := mustGen(t, nstest.Direct(str, "Other"), "a")
	if got := fl[0].FromString(); got != "Other" {
		t.Errorf("FromString() = %q, want %q", got, "Other")
	}
}

func TestCollisionsQualifyOnlyWhenAmbiguous(t *testing.T) {
	str := jenny.For[string]()
	file := func(s string) (*jenny.File, error) { return jenny.NewFile(s+".txt", nil), nil }

	tests := map[string]struct {
		a, b     jenny.Jenny[string]
		want     []string
		dontWant string
	}{
		"same name, different packages": {
			str.New("Same", file), nstest.Direct(str, "Same"),
			[]string{"'" + thisPkg + ".Same'", "'" + otherPkg + ".Same'"}, "",
		},
		"different names": {
			str.New("One", file), nstest.Direct(str, "Two"),
			[]string{"'One'", "'Two'"}, otherPkg,
		},
	}
	for name, tt := range tests {
		t.Run("Pipeline/"+name, func(t *testing.T) {
			_, err := jenny.Pipeline(tt.a, tt.b).Generate("a")
			checkQualified(t, err, tt.want, tt.dontWant)
		})
		t.Run("FS.Add/"+name, func(t *testing.T) {
			jfs, err := tt.a.GenerateFS("a")
			if err != nil {
				t.Fatal(err)
			}
			fl := mustGen(t, tt.b, "a")
			err = jfs.Add(fl...)
			// FS.Add quotes jenny names with %q.
			want := make([]string, len(tt.want))
			for i, w := range tt.want {
				want[i] = `"` + strings.Trim(w, "'") + `"`
			}
			checkQualified(t, err, want, tt.dontWant)
		})
	}
}

func checkQualified(t *testing.T, err error, want []string, dontWant string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a collision error")
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %s", err, w)
		}
	}
	if dontWant != "" && strings.Contains(err.Error(), dontWant) {
		t.Errorf("error %q should not mention %s", err, dontWant)
	}
}
