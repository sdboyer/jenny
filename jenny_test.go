package jenny_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

type Group struct {
	Name  string
	Types []Type
}

type Type struct {
	Name string
}

// paths returns the RelativePath of each File, in order.
func paths(fl jenny.Files) []string {
	out := make([]string, len(fl))
	for i, f := range fl {
		out[i] = f.RelativePath
	}
	return out
}

// contents returns a map of RelativePath to Data.
func contents(fl jenny.Files) map[string]string {
	out := make(map[string]string, len(fl))
	for _, f := range fl {
		out[f.RelativePath] = string(f.Data)
	}
	return out
}

func mustGen[I any](t *testing.T, j jenny.Jenny[I], in ...I) jenny.Files {
	t.Helper()
	fl, err := j.Generate(in...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	return fl
}

var ty = jenny.For[Type](jenny.InputName(func(t Type) string { return t.Name }))

func o2o(t Type) (*jenny.File, error) {
	return jenny.NewFile(t.Name+".o2o", []byte(t.Name)), nil
}

func o2m(t Type) (jenny.Files, error) {
	return jenny.Files{
		*jenny.NewFile(t.Name+".o2m.1", []byte(t.Name)),
		*jenny.NewFile(t.Name+".o2m.2", []byte(t.Name)),
	}, nil
}

func m2o(ts ...Type) (*jenny.File, error) {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	return jenny.NewFile("index.m2o", []byte(strings.Join(names, ","))), nil
}

func m2m(ts ...Type) (jenny.Files, error) {
	fl := make(jenny.Files, 0, len(ts))
	for _, t := range ts {
		fl = append(fl, *jenny.NewFile(t.Name+".m2m", []byte(t.Name)))
	}
	return fl, nil
}

var ab = []Type{{Name: "a"}, {Name: "b"}}

func TestNewArities(t *testing.T) {
	tests := map[string]struct {
		j    jenny.Jenny[Type]
		want []string
	}{
		"o2o": {ty.New("O2O", o2o), []string{"a.o2o", "b.o2o"}},
		"o2m": {ty.New("O2M", o2m), []string{"a.o2m.1", "a.o2m.2", "b.o2m.1", "b.o2m.2"}},
		"m2o": {ty.New("M2O", m2o), []string{"index.m2o"}},
		"m2m": {ty.New("M2M", m2m), []string{"a.m2m", "b.m2m"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fl := mustGen(t, tt.j, ab...)
			if diff := cmp.Diff(tt.want, paths(fl)); diff != "" {
				t.Errorf("paths (-want +got):\n%s", diff)
			}
			for _, f := range fl {
				if got := f.FromString(); got != strings.ToUpper(name) {
					t.Errorf("%s: From is %q, want %q", f.RelativePath, got, strings.ToUpper(name))
				}
			}
		})
	}

	if got := contents(mustGen(t, ty.New("M2O", m2o), ab...))["index.m2o"]; got != "a,b" {
		t.Errorf("many-input jenny got inputs %q, want %q", got, "a,b")
	}
}

func TestNewEmptyOutputs(t *testing.T) {
	tests := map[string]jenny.Jenny[Type]{
		"nil File":     ty.New("x", func(Type) (*jenny.File, error) { return nil, nil }),
		"zero File":    ty.New("x", func(Type) (*jenny.File, error) { return &jenny.File{}, nil }),
		"nil Files":    ty.New("x", func(Type) (jenny.Files, error) { return nil, nil }),
		"m2o nil File": ty.New("x", func(...Type) (*jenny.File, error) { return nil, nil }),
	}
	for name, j := range tests {
		t.Run(name, func(t *testing.T) {
			if fl := mustGen(t, j, ab...); len(fl) != 0 {
				t.Errorf("expected no files, got %v", paths(fl))
			}
		})
	}
}

func TestNoInputsNoCall(t *testing.T) {
	called := false
	j := ty.New("M2O", func(...Type) (*jenny.File, error) {
		called = true
		return nil, nil
	})
	mustGen(t, j)
	if called {
		t.Error("many-input jenny was called with zero inputs")
	}
}

func TestZeroJenny(t *testing.T) {
	var j jenny.Jenny[Type]
	if fl := mustGen(t, j, ab...); len(fl) != 0 {
		t.Errorf("zero Jenny generated %v", paths(fl))
	}
}

func TestNewEmptyNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for empty name")
		}
	}()
	ty.New("", o2o)
}

func TestNewErrors(t *testing.T) {
	boom := errors.New("boom")
	j := ty.New("Fails", func(t Type) (*jenny.File, error) {
		if t.Name == "b" {
			return nil, boom
		}
		return jenny.NewFile(t.Name, nil), nil
	})
	_, err := j.Generate(ab...)
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
	if want := `Fails: boom for input "b"`; err.Error() != want {
		t.Errorf("error is %q, want %q", err, want)
	}

	// Without InputName, errors aren't labeled with an input.
	_, err = jenny.For[Type]().New("Fails", func(Type) (*jenny.File, error) { return nil, boom }).Generate(ab[0])
	if want := "Fails: boom"; err == nil || err.Error() != want {
		t.Errorf("error is %v, want %q", err, want)
	}
}

func TestNewInvalidFiles(t *testing.T) {
	j := ty.New("Dupes", func(...Type) (jenny.Files, error) {
		return jenny.Files{*jenny.NewFile("x", nil), *jenny.NewFile("X", nil)}, nil
	})
	if _, err := j.Generate(ab...); err == nil {
		t.Error("expected error for case-insensitive duplicate paths")
	}
}

func TestNewDoesNotMutateGenOutput(t *testing.T) {
	cached := jenny.Files{*jenny.NewFile("a", nil)}
	j := ty.New("Cached", func(Type) (jenny.Files, error) { return cached, nil })
	for range 3 {
		fl := mustGen(t, j, ab[0])
		if got := fl[0].FromString(); got != "Cached" {
			t.Fatalf("From is %q, want %q", got, "Cached")
		}
	}
	if len(cached[0].From) != 0 {
		t.Errorf("gen func's returned Files were mutated: From is %q", cached[0].FromString())
	}
}

func TestWrappersApplyInOrder(t *testing.T) {
	suffix := func(s string) jenny.FileMapper {
		return func(f jenny.File) (jenny.File, error) {
			f.Data = append(append([]byte{}, f.Data...), s...)
			return f, nil
		}
	}
	userWrap := func(j jenny.Jenny[Type]) jenny.Jenny[Type] { return j.Prefix("user") }

	j := ty.New("O2O", o2o).Prefix("a").Map(suffix("+1")).Prefix("b").Map(suffix("+2")).With(userWrap)
	want := map[string]string{"user/b/a/a.o2o": "a+1+2"}
	if diff := cmp.Diff(want, contents(mustGen(t, j, ab[0]))); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}

	// wrappers are transparent in From
	if got := mustGen(t, j, ab[0])[0].FromString(); got != "O2O" {
		t.Errorf("From is %q, want %q", got, "O2O")
	}
}

func TestMapError(t *testing.T) {
	boom := errors.New("boom")
	j := ty.New("O2O", o2o).Map(func(jenny.File) (jenny.File, error) { return jenny.File{}, boom })
	if _, err := j.Generate(ab...); !errors.Is(err, boom) {
		t.Errorf("expected wrapped boom, got %v", err)
	}
}

func TestGenerateFS(t *testing.T) {
	jfs, err := ty.New("M2M", m2m).GenerateFS(ab...)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"a.m2m", "b.m2m"}, paths(jfs.AsFiles())); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}
