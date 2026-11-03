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
	tr, ok := fl[0].(jenny.Tree[words])
	if !ok {
		t.Fatalf("got %T, want jenny.Tree[words]", fl[0])
	}
	if tr.Path() != "out/a.txt" || tr.FromString() != "Words" {
		t.Errorf("Tree not placed and stamped: %s (%s)", tr.Path(), tr.FromString())
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
			jenny.NewClaim("*.words"),
		}, nil
	})

	fl := mustGen(t, j.Map(shout), ab[0])
	want := []string{
		`a.words (Mixed) jenny.Tree[github.com/sdboyer/jenny_test.words] "HI A"`,
		`a.num (Mixed) jenny.Tree[int] "n"`,
		`a.bytes (Mixed) jenny.Bytes "bytes"`,
		`*.words (Mixed) jenny.Claim ""`,
	}
	if diff := cmp.Diff(want, describe(fl)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}

	// Once rendered to Bytes, a Tree is no longer one, so a TreeMapper after a
	// BytesMapper passes it through.
	exclaim := jenny.BytesMapper(func(_ jenny.File, b []byte) ([]byte, error) { return append(b, '!'), nil })
	fl = mustGen(t, j.Map(exclaim).Map(shout), ab[0])
	if got := contents(fl)["a.words"]; got != "hi a!" {
		t.Errorf("got %q, want %q", got, "hi a!")
	}
	if _, ok := fl[0].(jenny.Bytes); !ok {
		t.Errorf("BytesMapper returned %T, want jenny.Bytes", fl[0])
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
