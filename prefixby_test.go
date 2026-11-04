package jenny_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

// dirOf places each Type under a directory named for it.
func dirOf(t Type) string { return "d" + t.Name }

func TestPrefixByPlacesEachInput(t *testing.T) {
	tests := map[string]struct {
		j    jenny.Jenny[Type]
		want map[string]string
	}{
		"one to one": {
			ty.PrefixBy(dirOf, ty.New("O2O", o2o)),
			map[string]string{"da/a.o2o": "a", "db/b.o2o": "b"},
		},
		"one to many": {
			ty.PrefixBy(dirOf, ty.New("O2M", o2m)),
			map[string]string{"da/a.o2m.1": "a", "da/a.o2m.2": "a", "db/b.o2m.1": "b", "db/b.o2m.2": "b"},
		},
		// A many-input jenny is called once per input, with that input alone.
		"many to one": {
			ty.PrefixBy(dirOf, ty.New("M2O", m2o)),
			map[string]string{"da/index.m2o": "a", "db/index.m2o": "b"},
		},
		"many to many": {
			ty.PrefixBy(dirOf, ty.New("M2M", m2m)),
			map[string]string{"da/a.m2m": "a", "db/b.m2m": "b"},
		},
		"pipeline": {
			ty.PrefixBy(dirOf, ty.New("O2O", o2o), ty.New("M2O", m2o)),
			map[string]string{"da/a.o2o": "a", "da/index.m2o": "a", "db/b.o2o": "b", "db/index.m2o": "b"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, contents(mustGen(t, tt.j, ab...))); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

// A many-input jenny that should see every input goes beside PrefixBy.
func TestPrefixByManyInputBeside(t *testing.T) {
	j := jenny.Pipeline(
		ty.PrefixBy(dirOf, ty.New("O2O", o2o)),
		ty.New("M2O", m2o),
	)
	want := map[string]string{"da/a.o2o": "a", "db/b.o2o": "b", "index.m2o": "a,b"}
	if diff := cmp.Diff(want, contents(mustGen(t, j, ab...))); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestPrefixByKeepsFrom(t *testing.T) {
	for _, f := range mustGen(t, ty.PrefixBy(dirOf, ty.New("O2O", o2o)), ab...) {
		if got := f.FromString(); got != "O2O" {
			t.Errorf("%s: From is %q, want %q", f.Path(), got, "O2O")
		}
	}
}

func TestPrefixByOrdering(t *testing.T) {
	upper := jenny.BytesMapper(func(f jenny.File, _ []byte) ([]byte, error) {
		return []byte(strings.ToUpper(f.Path())), nil
	})
	o := ty.New("O2O", o2o)
	tests := map[string]struct {
		j    jenny.Jenny[Type]
		want map[string]string
	}{
		"Prefix outside": {
			ty.PrefixBy(dirOf, o).Prefix("go"),
			map[string]string{"go/da/a.o2o": "a"},
		},
		"Prefix inside": {
			ty.PrefixBy(dirOf, o.Prefix("go")),
			map[string]string{"da/go/a.o2o": "a"},
		},
		"nested": {
			ty.PrefixBy(func(Type) string { return "outer" }, ty.PrefixBy(dirOf, o)),
			map[string]string{"outer/da/a.o2o": "a"},
		},
		// Map sees paths as they are where it's applied.
		"Map inside": {
			ty.PrefixBy(dirOf, o.Map(upper)),
			map[string]string{"da/a.o2o": "A.O2O"},
		},
		"Map outside": {
			ty.PrefixBy(dirOf, o).Map(upper),
			map[string]string{"da/a.o2o": "DA/A.O2O"},
		},
		"With": {
			o.With(func(j jenny.Jenny[Type]) jenny.Jenny[Type] { return ty.PrefixBy(dirOf, j) }).Prefix("go"),
			map[string]string{"go/da/a.o2o": "a"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, contents(mustGen(t, tt.j, ab[0]))); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

// PrefixBy sees the inputs it's given: inside Adapt, the adapted inputs;
// outside it, the parent's.
func TestPrefixByAdapt(t *testing.T) {
	split := func(g Group) ([]Type, error) { return g.Types, nil }
	groupDir := func(g Group) string { return g.Name }
	tests := map[string]struct {
		j    jenny.Jenny[Group]
		want []string
	}{
		"inside": {
			gr.Adapt(split, ty.PrefixBy(dirOf, ty.New("M2O", m2o))),
			[]string{"da/index.m2o", "db/index.m2o", "dc/index.m2o"},
		},
		"outside": {
			gr.PrefixBy(groupDir, gr.Adapt(split, ty.New("M2O", m2o))),
			[]string{"g1/index.m2o", "g2/index.m2o"},
		},
		"both": {
			gr.PrefixBy(groupDir, gr.Adapt(split, ty.PrefixBy(dirOf, ty.New("M2O", m2o)))),
			[]string{"g1/da/index.m2o", "g1/db/index.m2o", "g2/dc/index.m2o"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, paths(mustGen(t, tt.j, groups...))); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

// For every directory Prefix accepts, PrefixBy places files exactly where
// Prefix does, and it rejects every directory Prefix rejects.
func TestPrefixByMatchesPrefix(t *testing.T) {
	j := ty.New("O2O", o2o)
	for _, dir := range []string{"", ".", "./", "a", "a/b", "a/", "./a", "a/../b", "a/..", "a//b", `pkg\api`, "C:", "C:/pkg", "/abs", "..", "../up", "a/../..", "nul", "pkg/aux"} {
		t.Run(dir, func(t *testing.T) {
			want, wantErr := j.Prefix(dir).Generate(ab...)
			got, gotErr := ty.PrefixBy(func(Type) string { return dir }, j).Generate(ab...)
			if (wantErr == nil) != (gotErr == nil) {
				t.Fatalf("Prefix error %v, PrefixBy error %v", wantErr, gotErr)
			}
			if wantErr != nil {
				return
			}
			if diff := cmp.Diff(paths(want), paths(got)); diff != "" {
				t.Errorf("(-Prefix +PrefixBy):\n%s", diff)
			}
		})
	}
}

func TestPrefixByEmptyIsNoPrefix(t *testing.T) {
	dir := func(t Type) string {
		if t.Name == "a" {
			return ""
		}
		return dirOf(t)
	}
	want := []string{"a.o2o", "db/b.o2o"}
	if diff := cmp.Diff(want, paths(mustGen(t, ty.PrefixBy(dir, ty.New("O2O", o2o)), ab...))); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestPrefixByInvalidDir(t *testing.T) {
	dir := func(t Type) string {
		if t.Name == "b" {
			return "../up"
		}
		return dirOf(t)
	}
	j := ty.New("O2O", o2o)

	_, err := ty.PrefixBy(dir, j).Generate(ab...)
	want := `PrefixBy: ../up: File paths must be clean and slash-separated, with no '.', '..' or empty elements and no backslashes for input "b"`
	if err == nil || err.Error() != want {
		t.Errorf("error is %v, want %q", err, want)
	}

	// Without InputName, errors aren't labeled with an input.
	_, err = jenny.For[Type]().PrefixBy(dir, j).Generate(ab...)
	if want := strings.TrimSuffix(want, ` for input "b"`); err == nil || err.Error() != want {
		t.Errorf("error is %v, want %q", err, want)
	}

	// The directory is checked even for an input that generates nothing.
	nothing := ty.New("Nothing", func(Type) (jenny.File, error) { return nil, nil })
	if _, err := ty.PrefixBy(dir, nothing).Generate(ab...); err == nil {
		t.Error("expected error for invalid directory")
	}
}

func TestPrefixByErrors(t *testing.T) {
	e1, e2 := errors.New("e1"), errors.New("e2")
	j := ty.New("Fails", func(t Type) (jenny.File, error) {
		if t.Name == "a" {
			return nil, e1
		}
		return nil, e2
	})
	_, err := ty.PrefixBy(dirOf, j).Generate(ab...)
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("expected both errors, got %v", err)
	}
	// Errors from the jennies are labeled once, by the jennies themselves.
	if want := "Fails: e1 for input \"a\"\nFails: e2 for input \"b\""; err.Error() != want {
		t.Errorf("error is %q, want %q", err, want)
	}

	bad := func(t Type) string {
		if t.Name == "a" {
			return "/abs"
		}
		return dirOf(t)
	}
	_, err = ty.PrefixBy(bad, j).Generate(ab...)
	if errors.Is(err, e1) || !errors.Is(err, e2) || !strings.Contains(err.Error(), `PrefixBy: /abs: File paths must be relative for input "a"`) {
		t.Errorf("expected the directory error for a and e2 for b, got %v", err)
	}
}

func TestPrefixByCollisions(t *testing.T) {
	tests := map[string]func(Type) string{
		"same dir":         func(Type) string { return "same" },
		"case-insensitive": func(t Type) string { return map[string]string{"a": "dir", "b": "DIR"}[t.Name] },
		"both empty":       func(Type) string { return "" },
	}
	for name, dir := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ty.PrefixBy(dir, ty.New("M2O", m2o)).Generate(ab...)
			if err == nil || !strings.Contains(err.Error(), "index.m2o from jennies: 'M2O', 'M2O'") {
				t.Errorf("expected path collision error, got %v", err)
			}
		})
	}

	// PrefixBy's files share a namespace with its siblings in a pipeline.
	j := jenny.Pipeline(
		ty.PrefixBy(dirOf, ty.New("O2O", o2o)),
		ty.New("Other", func(...Type) (jenny.File, error) { return jenny.NewFile("da/a.o2o", nil), nil }),
	)
	if _, err := j.Generate(ab...); err == nil {
		t.Error("expected path collision error with a sibling")
	}
}

func TestPrefixByNoInputsNoCall(t *testing.T) {
	called := false
	j := ty.PrefixBy(func(Type) string {
		called = true
		return "d"
	}, ty.New("M2O", func(...Type) (jenny.File, error) {
		called = true
		return nil, nil
	}))
	mustGen(t, j)
	if called {
		t.Error("PrefixBy ran with zero inputs")
	}
}
