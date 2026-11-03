package jenny_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

// The nil-interface trap: a nil *Bytes returned as a File is not == nil.
// Authors must never hit it; jenny treats a nil pointer exactly as a nil File
// wherever user code hands it Files.

// maybe returns a nil *Bytes unless yes, the "maybe nil" pattern that worked
// with *File before File was an interface.
func maybe(name string, yes bool) *jenny.Bytes {
	var out *jenny.Bytes
	if yes {
		b := jenny.NewFile(name, []byte(name))
		out = &b
	}
	return out
}

func maybeTree(name string, yes bool) *jenny.Tree[words] {
	var out *jenny.Tree[words]
	if yes {
		t := jenny.NewTree(name, words{name}, renderWords)
		out = &t
	}
	return out
}

func TestNilPointersAreNilFiles(t *testing.T) {
	yes := func(t Type) bool { return t.Name == "a" }

	tests := map[string]struct {
		j    jenny.Jenny[Type]
		want []string
	}{
		"one to one, *Bytes": {ty.New("P", func(t Type) (jenny.File, error) {
			return maybe(t.Name+".txt", yes(t)), nil
		}), []string{`a.txt (P) jenny.Bytes "a.txt"`}},
		"one to one, *Tree": {ty.New("P", func(t Type) (jenny.File, error) {
			return maybeTree(t.Name+".txt", yes(t)), nil
		}), []string{`a.txt (P) jenny.Tree[github.com/sdboyer/jenny_test.words] "a.txt"`}},
		"many to one, nil": {ty.New("P", func(...Type) (jenny.File, error) {
			return maybe("x", false), nil
		}), []string{}},
		"many to one, non-nil": {ty.New("P", func(...Type) (jenny.File, error) {
			return maybe("x", true), nil
		}), []string{`x (P) jenny.Bytes "x"`}},
		"one to many, non-nil pointers": {ty.New("P", func(t Type) (jenny.Files, error) {
			return jenny.Files{maybe(t.Name+".1", true), maybeTree(t.Name+".2", true)}, nil
		}), []string{
			`a.1 (P) jenny.Bytes "a.1"`, `a.2 (P) jenny.Tree[github.com/sdboyer/jenny_test.words] "a.2"`,
			`b.1 (P) jenny.Bytes "b.1"`, `b.2 (P) jenny.Tree[github.com/sdboyer/jenny_test.words] "b.2"`,
		}},
		"many to many, non-nil pointers": {ty.New("P", func(...Type) (jenny.Files, error) {
			return jenny.Files{maybe("x", true)}, nil
		}), []string{`x (P) jenny.Bytes "x"`}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := describe(mustGen(t, tt.j, ab...))
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

// A nil element of a Files is an error, whether it's nil or a nil pointer. It
// is never a panic.
func TestNilPointersInFiles(t *testing.T) {
	tests := map[string]jenny.Jenny[Type]{
		"one to many, *Bytes": ty.New("P", func(t Type) (jenny.Files, error) {
			return jenny.Files{maybe(t.Name, false)}, nil
		}),
		"many to many, *Tree": ty.New("P", func(...Type) (jenny.Files, error) {
			return jenny.Files{maybeTree("x", false)}, nil
		}),
		"many to many, nil": ty.New("P", func(...Type) (jenny.Files, error) {
			return jenny.Files{nil}, nil
		}),
	}
	for name, j := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := j.Generate(ab...); err == nil {
				t.Error("expected error for a nil File in Files")
			}
		})
	}

	if err := (jenny.Files{maybe("x", false)}).Validate(); err == nil {
		t.Error("Files.Validate accepted a nil *Bytes")
	}
	if err := jenny.NewFS().Add(maybeTree("x", false)); err == nil {
		t.Error("FS.Add accepted a nil *Tree")
	}
	// any(got) != nil checks for a nil interface exactly, which is what Move
	// promises; jenny.IsNil would also accept a nil pointer.
	if got := jenny.Move(maybe("x", false), "y"); any(got) != nil {
		t.Errorf("Move of a nil *Bytes = %#v, want nil", got)
	}
}

func TestFileMapperNilPointers(t *testing.T) {
	tests := map[string]jenny.FileMapper{
		"nil":    func(jenny.File) (jenny.File, error) { return nil, nil },
		"*Bytes": func(jenny.File) (jenny.File, error) { return maybe("x", false), nil },
		"*Tree":  func(jenny.File) (jenny.File, error) { return maybeTree("x", false), nil },
	}
	for name, fm := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ty.New("O2O", o2o).Map(fm).Generate(ab...); err == nil {
				t.Error("expected error for a FileMapper returning a nil File")
			}
			jfs := fsWith(t, jenny.NewFile("a", nil, src("t")))
			if _, err := jfs.Map(fm); err == nil {
				t.Error("expected error from FS.Map for a FileMapper returning a nil File")
			}
		})
	}

	// A non-nil pointer is the File it points to, so a TreeMapper after it
	// still finds its Tree.
	toPointer := func(f jenny.File) (jenny.File, error) {
		t := jenny.NewTree(f.Path(), words{"from", "pointer"}, renderWords, f.From()...)
		return &t, nil
	}
	shout := jenny.TreeMapper(func(_ jenny.File, w words) (words, error) { return append(w, "!"), nil })
	fl := mustGen(t, ty.New("O2O", o2o).Map(toPointer).Map(shout), ab[0])
	if diff := cmp.Diff([]string{`a.o2o (O2O) jenny.Tree[github.com/sdboyer/jenny_test.words] "from pointer !"`}, describe(fl)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}
