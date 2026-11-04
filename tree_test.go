package jenny_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

// words is a stand-in for an AST: a value that renders to bytes.
type words []string

func renderWords(w words) ([]byte, error) { return []byte(strings.Join(w, " ")), nil }

func TestTreeIsNotRenderedUntilNeeded(t *testing.T) {
	renders := 0
	render := func(w words) ([]byte, error) {
		renders++
		return renderWords(w)
	}
	j := ty.New("Words", func(t Type) (jenny.File, error) {
		return jenny.NewTree(t.Name+".txt", words{"hello", t.Name}, render), nil
	}).Prefix("out")

	fl := mustGen(t, j, ab...)
	if renders != 0 {
		t.Fatalf("Generate rendered %d Trees, want 0", renders)
	}
	if _, ok := jenny.TreeOf[words](fl[0]); !ok {
		t.Fatalf("got %T, want jenny.Tree[words]", fl[0])
	}
	if fl[0].Path() != "out/a.txt" || fl[0].FromString() != "Words" {
		t.Errorf("Tree not placed and stamped: %s (%s)", fl[0].Path(), fl[0].FromString())
	}

	jfs, err := j.GenerateFS(ab...)
	if err != nil {
		t.Fatal(err)
	}
	if renders != 2 {
		t.Errorf("GenerateFS rendered %d Trees, want 2", renders)
	}
	want := map[string]string{"out/a.txt": "hello a", "out/b.txt": "hello b"}
	if diff := cmp.Diff(want, contents(jfs.AsFiles())); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestTreeMapper(t *testing.T) {
	shout := jenny.TreeMapper(func(_ jenny.File, w words) (words, error) {
		out := make(words, len(w))
		for i, s := range w {
			out[i] = strings.ToUpper(s)
		}
		return out, nil
	})
	j := ty.New("Mixed", func(t Type) (jenny.Files, error) {
		return jenny.Files{
			jenny.NewTree(t.Name+".words", words{"hi", t.Name}, renderWords),
			jenny.NewTree(t.Name+".num", 42, func(n int) ([]byte, error) { return []byte("n"), nil }),
			jenny.NewFile(t.Name+".bytes", []byte("bytes")),
		}, nil
	})

	fl := mustGen(t, j.Map(shout), ab[0])
	want := []string{
		`a.words (Mixed) jenny.Tree[github.com/sdboyer/jenny_test.words] "HI A"`,
		`a.num (Mixed) jenny.Tree[int] "n"`,
		`a.bytes (Mixed) jenny.Bytes "bytes"`,
	}
	if diff := cmp.Diff(want, describe(fl)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

// A TreeMapper after a BytesMapper is an error for a file that was a Tree of
// its type, and only for that file: the BytesMapper rendered away the tree the
// TreeMapper was meant for.
func TestTreeMapperAfterBytesMapper(t *testing.T) {
	exclaim := jenny.BytesMapper(func(_ jenny.File, b []byte) ([]byte, error) { return append(b, '!'), nil })
	shout := jenny.TreeMapper(func(_ jenny.File, w words) (words, error) { return append(w, "SHOUT"), nil })
	gen := func(fl ...jenny.File) jenny.Jenny[Type] {
		return ty.New("Mixed", func(Type) (jenny.Files, error) { return fl, nil })
	}
	num := jenny.NewTree("a.num", 42, func(n int) ([]byte, error) { return []byte("n"), nil })
	wordTree := jenny.NewTree("a.words", words{"hi"}, renderWords)

	// Files that were never a Tree[words] pass through, rendered or not.
	fl := mustGen(t, gen(num, jenny.NewFile("a.bytes", []byte("bytes"))).Map(exclaim).Map(exclaim).Map(shout), ab[0])
	want := []string{`a.num (Mixed) jenny.Bytes "n!!"`, `a.bytes (Mixed) jenny.Bytes "bytes!!"`}
	if diff := cmp.Diff(want, describe(fl)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}

	// A Tree[words] rendered by a BytesMapper is an error naming the file,
	// however many BytesMappers, Moves and Prefixes it went through since.
	moved := jenny.FileMapper(func(f jenny.File) (jenny.File, error) { return jenny.Move(f, "moved/"+f.Path()), nil })
	_, err := gen(wordTree).Map(exclaim).Map(exclaim).Map(moved).Prefix("out").Map(shout).Generate(ab[0])
	if err == nil {
		t.Fatal("expected an error for a TreeMapper after a BytesMapper rendered its Tree")
	}
	for _, s := range []string{"out/moved/a.words", "BytesMapper", "TreeMapper", "jenny.Tree[github.com/sdboyer/jenny_test.words]"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error doesn't mention %q: %s", s, err)
		}
	}

	// In the right order, it's fine.
	fl = mustGen(t, gen(wordTree).Map(shout).Map(exclaim), ab[0])
	if diff := cmp.Diff([]string{`a.words (Mixed) jenny.Bytes "hi SHOUT!"`}, describe(fl)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}

	// A file rebuilt from the bytes is new content, with no record of a Tree.
	rebuilt := jenny.FileMapper(func(f jenny.File) (jenny.File, error) {
		data, err := f.Render()
		return jenny.NewFile(f.Path(), data, f.From()...), err
	})
	if _, err := gen(wordTree).Map(exclaim).Map(rebuilt).Map(shout).Generate(ab[0]); err != nil {
		t.Errorf("unexpected error for a file rebuilt from bytes: %s", err)
	}
}

// The helpers that ask what a File holds treat a pointer as its value, and a
// nil pointer as nil.
func TestFileHelpers(t *testing.T) {
	b := jenny.NewFile("b", []byte("data"))
	tr := jenny.NewTree("t", words{"w"}, renderWords)
	var nilBytes *jenny.Bytes
	var nilTree *jenny.Tree[words]

	tests := map[string]struct {
		f     jenny.File
		isNil bool
		data  string // "" means BytesOf reports false
		tree  words  // nil means TreeOf[words] reports false
	}{
		"nil":            {f: nil, isNil: true},
		"nil *Bytes":     {f: nilBytes, isNil: true},
		"nil *Tree":      {f: nilTree, isNil: true},
		"Bytes":          {f: b, data: "data"},
		"*Bytes":         {f: &b, data: "data"},
		"Tree":           {f: tr, tree: words{"w"}},
		"*Tree":          {f: &tr, tree: words{"w"}},
		"Tree, other T":  {f: jenny.NewTree("n", 1, func(int) ([]byte, error) { return nil, nil })},
		"*Tree, other T": {f: new(jenny.NewTree("n", 1, func(int) ([]byte, error) { return nil, nil }))},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := jenny.IsNil(tt.f); got != tt.isNil {
				t.Errorf("IsNil = %v, want %v", got, tt.isNil)
			}
			data, ok := jenny.BytesOf(tt.f)
			if ok != (tt.data != "") || string(data) != tt.data {
				t.Errorf("BytesOf = %q, %v, want %q", data, ok, tt.data)
			}
			v, ok := jenny.TreeOf[words](tt.f)
			if ok != (tt.tree != nil) {
				t.Errorf("TreeOf ok = %v, want %v", ok, tt.tree != nil)
			}
			if diff := cmp.Diff(tt.tree, v); diff != "" {
				t.Errorf("TreeOf (-want +got):\n%s", diff)
			}
		})
	}
}

// FileMappers made by jenny take pointers as their values, since user code
// can call them directly, as ExampleFileMapper's goHeader does, and reject nil.
func TestMakersNormalize(t *testing.T) {
	shout := jenny.TreeMapper(func(_ jenny.File, w words) (words, error) { return append(w, "!"), nil })
	exclaim := jenny.BytesMapper(func(_ jenny.File, b []byte) ([]byte, error) { return append(b, '!'), nil })
	tr := jenny.NewTree("t", words{"w"}, renderWords, src("j"))

	got, err := shout(&tr)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := jenny.TreeOf[words](got); !cmp.Equal(v, words{"w", "!"}) {
		t.Errorf("TreeMapper of a *Tree = %v, want [w !]", v)
	}
	got, err = exclaim(&tr)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := jenny.BytesOf(got); string(data) != "w!" {
		t.Errorf("BytesMapper of a *Tree = %q, want %q", data, "w!")
	}

	var nilTree *jenny.Tree[words]
	for name, fm := range map[string]jenny.FileMapper{"TreeMapper": shout, "BytesMapper": exclaim} {
		if _, err := fm(nilTree); err == nil {
			t.Errorf("%s accepted a nil *Tree", name)
		}
	}
}

func TestTreeErrors(t *testing.T) {
	boom := errors.New("boom")
	failing := ty.New("Bad", func(t Type) (jenny.File, error) {
		return jenny.NewTree(t.Name+".txt", words{}, func(words) ([]byte, error) { return nil, boom }), nil
	})

	_, err := failing.GenerateFS(ab[0])
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "a.txt") {
		t.Errorf("render error not reported with its path: %v", err)
	}

	noRender := ty.New("NoRender", func(Type) (jenny.File, error) {
		return jenny.NewTree[words]("a.txt", nil, nil), nil
	})
	if _, err := noRender.Generate(ab[0]); err == nil {
		t.Error("expected error for a Tree with no render func")
	}
}

func TestMove(t *testing.T) {
	renders := 0
	render := func(w words) ([]byte, error) {
		renders++
		return renderWords(w)
	}
	moveTests := jenny.FileMapper(func(f jenny.File) (jenny.File, error) {
		return jenny.Move(f, "tests/"+f.Path()), nil
	})
	j := ty.New("Words", func(t Type) (jenny.File, error) {
		return jenny.NewTree(t.Name+".txt", words{t.Name}, render), nil
	}).Map(moveTests).Prefix("out")

	fl := mustGen(t, j, ab[0])
	if renders != 0 {
		t.Errorf("Move rendered the Tree %d times", renders)
	}
	if _, ok := jenny.TreeOf[words](fl[0]); !ok || fl[0].Path() != "out/tests/a.txt" || fl[0].FromString() != "Words" {
		t.Errorf("got %s (%s) %T, want out/tests/a.txt (Words) as a Tree", fl[0].Path(), fl[0].FromString(), fl[0])
	}

	// Moved paths are validated like any other.
	bad := ty.New("O2O", o2o).Map(func(f jenny.File) (jenny.File, error) { return jenny.Move(f, "../x"), nil })
	if _, err := bad.Generate(ab[0]); err == nil {
		t.Error("expected error for a File moved outside the root")
	}
}
