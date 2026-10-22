package jenny_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

var gr = jenny.For[Group](jenny.InputName(func(g Group) string { return g.Name }))

var groups = []Group{
	{Name: "g1", Types: []Type{{Name: "a"}, {Name: "b"}}},
	{Name: "g2", Types: []Type{{Name: "c"}}},
}

func TestAdaptArities(t *testing.T) {
	// Each adapter is paired with an identical subtree containing a many-input
	// jenny, whose output reveals how many times the subtree ran and with
	// which inputs.
	index := func(ts ...Type) (*jenny.File, error) {
		names := make([]string, len(ts))
		for i, t := range ts {
			names[i] = t.Name
		}
		joined := strings.Join(names, ",")
		return jenny.NewFile("index-"+joined, []byte(joined)), nil
	}
	first := func(g Group) (Type, error) { return g.Types[0], nil }
	split := func(g Group) ([]Type, error) { return g.Types, nil }
	merge := func(gs ...Group) (Type, error) {
		names := make([]string, len(gs))
		for i, g := range gs {
			names[i] = g.Name
		}
		return Type{Name: strings.Join(names, "+")}, nil
	}
	splitAll := func(gs ...Group) ([]Type, error) {
		var ts []Type
		for _, g := range gs {
			ts = append(ts, g.Types...)
		}
		return ts, nil
	}

	tests := map[string]struct {
		j    jenny.Jenny[Group]
		want []string
	}{
		"one to one":   {gr.Adapt(first, ty.New("Index", index)), []string{"index-a", "index-c"}},
		"one to many":  {gr.Adapt(split, ty.New("Index", index)), []string{"index-a,b", "index-c"}},
		"many to one":  {gr.Adapt(merge, ty.New("Index", index)), []string{"index-g1+g2"}},
		"many to many": {gr.Adapt(splitAll, ty.New("Index", index)), []string{"index-a,b,c"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, paths(mustGen(t, tt.j, groups...))); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestAdaptSubtreeIsNotSpecial(t *testing.T) {
	sub := jenny.Pipeline(ty.New("O2O", o2o), ty.New("M2O", m2o).Prefix("idx"))
	direct := mustGen(t, sub, groups[0].Types...)
	adapted := mustGen(t, gr.Adapt(func(g Group) ([]Type, error) { return g.Types, nil }, sub), groups[0])
	if diff := cmp.Diff(direct, adapted); diff != "" {
		t.Errorf("(-direct +adapted):\n%s", diff)
	}
}

func TestAdaptDropsNil(t *testing.T) {
	type stringer interface{ String() string }

	t.Run("pointer", func(t *testing.T) {
		b := jenny.For[*Type]()
		gen := b.New("P", func(t *Type) (*jenny.File, error) { return jenny.NewFile(t.Name, nil), nil })
		// nil first, to make sure nilability isn't decided by the first value
		j := gr.Adapt(func(g Group) ([]*Type, error) { return []*Type{nil, &g.Types[0], nil}, nil }, gen)
		if diff := cmp.Diff([]string{"a", "c"}, paths(mustGen(t, j, groups...))); diff != "" {
			t.Errorf("(-want +got):\n%s", diff)
		}
	})

	t.Run("interface", func(t *testing.T) {
		b := jenny.For[stringer]()
		gen := b.New("I", func(s stringer) (*jenny.File, error) { return jenny.NewFile(s.String(), nil), nil })
		j := gr.Adapt(func(g Group) (stringer, error) {
			if g.Name == "g1" {
				return nil, nil
			}
			return named(g.Name), nil
		}, gen)
		if diff := cmp.Diff([]string{"g2"}, paths(mustGen(t, j, groups...))); diff != "" {
			t.Errorf("(-want +got):\n%s", diff)
		}
	})

	t.Run("non-nilable kept", func(t *testing.T) {
		j := gr.Adapt(func(Group) (Type, error) { return Type{}, nil }, ty.New("Z", func(...Type) (*jenny.File, error) {
			return jenny.NewFile("zero", nil), nil
		}))
		if diff := cmp.Diff([]string{"zero"}, paths(mustGen(t, j, groups[0]))); diff != "" {
			t.Errorf("(-want +got):\n%s", diff)
		}
	})

	t.Run("all dropped means subtree does not run", func(t *testing.T) {
		called := false
		j := gr.Adapt(func(Group) (*Type, error) { return nil, nil }, jenny.For[*Type]().New("M", func(...*Type) (*jenny.File, error) {
			called = true
			return nil, nil
		}))
		mustGen(t, j, groups...)
		if called {
			t.Error("subtree ran with zero inputs")
		}
	})
}

type named string

func (n named) String() string { return string(n) }

func TestAdaptErrors(t *testing.T) {
	boom := errors.New("bind failed")
	// The kind2pd case: the adapter itself fails.
	j := gr.Adapt(func(g Group) (Type, error) {
		if g.Name == "g2" {
			return Type{}, boom
		}
		return g.Types[0], nil
	}, ty.New("O2O", o2o))
	_, err := j.Generate(groups...)
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped adapter error, got %v", err)
	}
	if want := `adapter failed: bind failed for input "g2"`; err.Error() != want {
		t.Errorf("error is %q, want %q", err, want)
	}

	// Errors from within the subtree carry both inner and outer input names.
	j = gr.Adapt(func(g Group) ([]Type, error) { return g.Types, nil },
		ty.New("Fails", func(t Type) (*jenny.File, error) {
			if t.Name == "c" {
				return nil, boom
			}
			return nil, nil
		}))
	_, err = j.Generate(groups...)
	if want := `Fails: bind failed for input "c" for input "g2"`; err == nil || err.Error() != want {
		t.Errorf("error is %v, want %q", err, want)
	}
}

func TestNestedAdapt(t *testing.T) {
	type Field struct{ Name string }
	fi := jenny.For[Field]()

	j := jenny.Prefix("out",
		gr.New("GroupDoc", func(g Group) (*jenny.File, error) { return jenny.NewFile(g.Name+".md", nil), nil }),
		gr.Adapt(func(g Group) ([]Type, error) { return g.Types, nil },
			ty.New("TypeDoc", func(t Type) (*jenny.File, error) { return jenny.NewFile(t.Name+".md", nil), nil }).Prefix("types"),
			ty.Adapt(func(t Type) ([]Field, error) { return []Field{{Name: t.Name + "1"}, {Name: t.Name + "2"}}, nil },
				fi.New("FieldDoc", func(f Field) (*jenny.File, error) { return jenny.NewFile(f.Name+".md", nil), nil }),
			).Prefix("fields"),
		),
	)

	fl := mustGen(t, j, groups...)
	want := []string{
		"out/g1.md", "out/g2.md",
		// g1's subtree runs its jennies in order over [a, b], then g2's over [c]
		"out/types/a.md", "out/types/b.md",
		"out/fields/a1.md", "out/fields/a2.md", "out/fields/b1.md", "out/fields/b2.md",
		"out/types/c.md",
		"out/fields/c1.md", "out/fields/c2.md",
	}
	if diff := cmp.Diff(want, paths(fl)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	for _, f := range fl {
		want := map[string]string{"out/g": "GroupDoc", "out/t": "TypeDoc", "out/f": "FieldDoc"}[f.RelativePath[:5]]
		if got := f.FromString(); got != want {
			t.Errorf("%s: From is %q, want %q", f.RelativePath, got, want)
		}
	}
}

func Example_adapt() {
	gr := jenny.For[Group]()
	ty := jenny.For[Type]()

	gen := jenny.Pipeline(
		gr.New("GroupDocs", func(g Group) (*jenny.File, error) {
			return jenny.NewFile(g.Name+".md", []byte("# "+g.Name)), nil
		}).Prefix("docs"),
		gr.Adapt(func(g Group) ([]Type, error) { return g.Types, nil },
			ty.New("GoTypes", func(t Type) (*jenny.File, error) {
				return jenny.NewFile(t.Name+".go", []byte("type "+t.Name+" struct{}")), nil
			}).Prefix("go"),
		),
	)

	fs, err := gen.GenerateFS(groups...)
	if err != nil {
		panic(err)
	}
	for _, f := range fs.AsFiles() {
		fmt.Printf("%s (%s)\n", f.RelativePath, f.FromString())
	}
	// Output:
	// docs/g1.md (GroupDocs)
	// docs/g2.md (GroupDocs)
	// go/a.go (GoTypes)
	// go/b.go (GoTypes)
	// go/c.go (GoTypes)
}
