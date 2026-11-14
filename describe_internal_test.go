package jenny

import (
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The tests in this file are the only ones inside package jenny: they check
// the nodes that record how a Jenny was composed, which are unexported.

const thisFile = "describe_internal_test.go"

var (
	stringType = reflect.TypeFor[string]()
	intType    = reflect.TypeFor[int]()
)

// thisLine returns the line it's called on.
func thisLine() int {
	pc := make([]uintptr, 1)
	f, _ := runtime.CallersFrames(pc[:runtime.Callers(2, pc)]).Next()
	return f.Line
}

func genOne(s string) (File, error)       { return NewFile(s, nil), nil }
func genMany(s string) (Files, error)     { return Files{NewFile(s, nil)}, nil }
func genAll(ss ...string) (File, error)   { return NewFile("all", nil), nil }
func genAllMany(...string) (Files, error) { return nil, nil }

func genGeneric[T any](T) (File, error) { return nil, nil }

type gens struct{}

func (gens) Value(string) (File, error)    { return nil, nil }
func (*gens) Pointer(string) (File, error) { return nil, nil }

func addHeader(f File) (File, error) { return f, nil }

func prefixed(j Jenny[string]) Jenny[string] { return j.Prefix("wrapped") }

func lengths(s string) (int, error)          { return len(s), nil }
func allLengths(ss ...string) ([]int, error) { return nil, nil }

// node fields compared by these tests: all but the source positions of
// funcs, which are checked on their own.
func shape(n *node) any {
	type view struct {
		Op              op
		In, Out         reflect.Type
		Name, Dir       string
		ManyIn, ManyOut bool
		Fn              string
		Children        []any
	}
	v := view{Op: n.op, In: n.in, Out: n.out, Name: n.name, Dir: n.dir, ManyIn: n.manyIn, ManyOut: n.manyOut, Fn: n.fn.name}
	for _, c := range n.children {
		v.Children = append(v.Children, shape(c))
	}
	return v
}

func checkShape(t *testing.T, j Jenny[string], want *node) {
	t.Helper()
	if diff := cmp.Diff(shape(want), shape(j.describe()), cmp.Comparer(func(a, b reflect.Type) bool { return a == b })); diff != "" {
		t.Errorf("node mismatch (-want +got):\n%s", diff)
	}
}

func TestDescribeNew(t *testing.T) {
	str := For[string]()
	for _, tc := range []struct {
		j               Jenny[string]
		fn              string
		manyIn, manyOut bool
	}{
		{str.New("One", genOne), "genOne", false, false},
		{str.New("Many", genMany), "genMany", false, true},
		{str.New("All", genAll), "genAll", true, false},
		{str.New("AllMany", genAllMany), "genAllMany", true, true},
		{str.New("Generic", genGeneric[string]), "genGeneric", false, false},
		{str.New("Value", gens{}.Value), "gens.Value", false, false},
		{str.New("Pointer", (&gens{}).Pointer), "gens.Pointer", false, false},
	} {
		n := tc.j.describe()
		checkShape(t, tc.j, &node{op: opNew, in: stringType, name: n.name, fn: symbol{name: tc.fn}, manyIn: tc.manyIn, manyOut: tc.manyOut})
		if n.fn.pkg != jennyPkg() {
			t.Errorf("%s: func package is %q, want %q", n.name, n.fn.pkg, jennyPkg())
		}
	}

	if n := str.New("One", genOne).describe(); n.fn.file != thisFile || n.fn.line == 0 {
		t.Errorf("genOne is at %s:%d, want %s and a line", n.fn.file, n.fn.line, thisFile)
	}

	lit, line := str.New("Literal", func(string) (File, error) { return nil, nil }), thisLine()
	if fn := lit.describe().fn; fn != (symbol{pkg: jennyPkg(), file: thisFile, line: line}) {
		t.Errorf("a func literal is %+v, want no name, at %s:%d", fn, thisFile, line)
	}
}

func TestDescribeComposition(t *testing.T) {
	str := For[string]()
	one, all := str.New("One", genOne), str.New("All", genAll)
	newOne := &node{op: opNew, in: stringType, name: "One", fn: symbol{name: "genOne"}}
	newAll := &node{op: opNew, in: stringType, name: "All", fn: symbol{name: "genAll"}, manyIn: true}

	var zero Jenny[string]
	checkShape(t, zero, &node{op: opEmpty, in: stringType})
	checkShape(t, Pipeline[string](), &node{op: opPipeline, in: stringType})
	checkShape(t, Pipeline(one, zero), &node{op: opPipeline, in: stringType, children: []*node{newOne, {op: opEmpty, in: stringType}}})
	checkShape(t, one.Prefix("a").Prefix("b"), &node{op: opPrefix, in: stringType, dir: "b", children: []*node{{op: opPrefix, in: stringType, dir: "a", children: []*node{newOne}}}})
	checkShape(t, Prefix("p", one, all), &node{op: opPrefix, in: stringType, dir: "p", children: []*node{{op: opPipeline, in: stringType, children: []*node{newOne, newAll}}}})
	checkShape(t, one.Map(addHeader), &node{op: opMap, in: stringType, fn: symbol{name: "addHeader"}, children: []*node{newOne}})
	checkShape(t, one.Map(BytesMapper(nil)), &node{op: opMap, in: stringType, children: []*node{newOne}})
	checkShape(t, one.With(prefixed), &node{op: opWith, in: stringType, fn: symbol{name: "prefixed"}, children: []*node{{op: opPrefix, in: stringType, dir: "wrapped", children: []*node{newOne}}}})
	checkShape(t, str.PrefixBy(func(s string) string { return s }, one, all), &node{op: opPrefixBy, in: stringType, children: []*node{newOne, newAll}})

	num := For[int]()
	numOne := num.New("Num", func(int) (File, error) { return nil, nil })
	numShape := &node{op: opNew, in: intType, name: "Num"}
	for _, tc := range []struct {
		j               Jenny[string]
		fn              string
		manyIn, manyOut bool
	}{
		{str.Adapt(lengths, numOne), "lengths", false, false},
		{str.Adapt(func(string) ([]int, error) { return nil, nil }, numOne), "", false, true},
		{str.Adapt(func(...string) (int, error) { return 0, nil }, numOne), "", true, false},
		{str.Adapt(allLengths, numOne), "allLengths", true, true},
	} {
		checkShape(t, tc.j, &node{op: opAdapt, in: stringType, out: intType, fn: symbol{name: tc.fn}, manyIn: tc.manyIn, manyOut: tc.manyOut, children: []*node{numShape}})
	}
}

// A Jenny's behavior doesn't depend on its node: With keeps the jenny its
// wrapper returns, and a zero Jenny still generates nothing.
func TestDescribeKeepsBehavior(t *testing.T) {
	one := For[string]().New("One", genOne)
	fl, err := one.With(prefixed).Generate("x")
	if err != nil || len(fl) != 1 || fl[0].Path() != "wrapped/x" {
		t.Errorf("With(prefixed) made %v, %v; want wrapped/x", fl, err)
	}
	var zero Jenny[string]
	if fl, err := zero.With(func(j Jenny[string]) Jenny[string] { return j }).Generate("x"); err != nil || len(fl) != 0 {
		t.Errorf("a wrapped zero Jenny made %v, %v; want nothing", fl, err)
	}
	if _, err := one.With(func(Jenny[string]) Jenny[string] {
		return For[string]().New("Fails", func(string) (File, error) { return nil, errors.New("boom") })
	}).Generate("x"); err == nil {
		t.Error("the jenny a wrapper returns wasn't the one that ran")
	}
}

func TestSplitSymbol(t *testing.T) {
	for _, tc := range []struct{ sym, pkg, name string }{
		{"github.com/a/b.F", "github.com/a/b", "F"},
		{"github.com/a/b.T.M", "github.com/a/b", "T.M"},
		{"github.com/a/b.(*T).M", "github.com/a/b", "T.M"},
		{"github.com/a/b.(*T).M-fm", "github.com/a/b", "T.M"},
		{"github.com/a/b.T.M-fm", "github.com/a/b", "T.M"},
		{"github.com/a/b.F[...]", "github.com/a/b", "F"},
		{"github.com/a/b.(*T[...]).M", "github.com/a/b", "T.M"},
		{"github.com/a/b.Builder[...].New[...]", "github.com/a/b", "Builder.New"},
		{"gopkg.in/yaml%2ev3.Marshal", "gopkg.in/yaml.v3", "Marshal"},
		{"github.com/a/b.F.func1", "github.com/a/b", ""},
		{"github.com/a/b.F.func1.2", "github.com/a/b", ""},
		{"github.com/a/b.init.func3", "github.com/a/b", ""},
		{"github.com/a/b.glob..func1", "github.com/a/b", ""},
		{"github.com/a/b.T.M.func2", "github.com/a/b", ""},
		{"github.com/a/b.functional", "github.com/a/b", "functional"},
		{"main.main", "main", "main"},
		{"fmt.Println", "fmt", "Println"},
		{"", "", ""},
	} {
		pkg, name := splitSymbol(tc.sym)
		if pkg != tc.pkg || name != tc.name {
			t.Errorf("splitSymbol(%q) = %q, %q; want %q, %q", tc.sym, pkg, name, tc.pkg, tc.name)
		}
	}
}
