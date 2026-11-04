package jenny_test

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"strings"
	"testing"

	"github.com/sdboyer/jenny"
)

// Lineage and Schema stand in for a thema lineage, which holds several
// versions of a schema.
type Lineage struct {
	Name    string
	Schemas []Schema
}

type Schema struct {
	Major, Minor int
	Fields       []string
}

var lineages = []Lineage{
	{Name: "playlist", Schemas: []Schema{
		{Major: 0, Minor: 0, Fields: []string{"name"}},
		{Major: 1, Minor: 0, Fields: []string{"name", "interval"}},
	}},
}

func schemasOf(l Lineage) ([]Schema, error) { return l.Schemas, nil }

func versionDir(s Schema) string { return fmt.Sprintf("v%d_%d", s.Major, s.Minor) }

// goSpec is the Go source for one schema, in package pkg. Both lineage
// examples share it, as jennies share logic through plain helpers.
func goSpec(pkg string, s Schema) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\ntype Spec struct {\n", pkg)
	for _, f := range s.Fields {
		fmt.Fprintf(&b, "\t%s string\n", title(f))
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

// schemaGoTypes generates Go types for one schema. It knows nothing about
// lineages: it writes types.go in package schema, so it's also usable on its
// own over a single schema.
func schemaGoTypes(s Schema) (jenny.File, error) {
	src, err := goSpec("schema", s)
	if err != nil {
		return nil, err
	}
	return jenny.NewFile("types.go", src), nil
}

// goPackageFromDir sets each Go file's package clause to the name of the
// directory the file is in. Package names are Go's business, not jenny's, so
// this is a plain FileMapper. It needs no input, only the File's path, so it
// works wherever it's applied outside the placement it follows. Applied before
// the file has a directory, it fails rather than guessing.
var goPackageFromDir = jenny.BytesMapper(func(f jenny.File, data []byte) ([]byte, error) {
	if path.Ext(f.Path()) != ".go" {
		return data, nil
	}
	pkg := path.Base(path.Dir(f.Path()))
	if !token.IsIdentifier(pkg) {
		return nil, fmt.Errorf("directory %q is not a Go package name", pkg)
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, f.Path(), data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	af.Name.Name = pkg
	var b bytes.Buffer
	if err := format.Node(&b, fset, af); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
})

func printLineageFiles(fl jenny.Files, err error) {
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, f := range fl {
		data, err := f.Render()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Printf("== %s (%s)\n%s", f.Path(), f.FromString(), data)
	}
}

// The lineage jenny wraps the schema jenny, and only places its output.
// PrefixBy puts each schema's files in a directory named for its version, and
// goPackageFromDir names the Go package after that directory. The outer
// PrefixBy sees lineages and the inner one sees schemas, because each sees the
// inputs it's given.
func Example_lineagePrefixBy() {
	lin := jenny.For[Lineage](jenny.InputName(func(l Lineage) string { return l.Name }))
	sch := jenny.For[Schema](jenny.InputName(versionDir))

	gen := lin.PrefixBy(func(l Lineage) string { return l.Name },
		lin.Adapt(schemasOf,
			sch.PrefixBy(versionDir, sch.New("GoTypes", schemaGoTypes)),
		),
	).Map(goPackageFromDir)

	printLineageFiles(gen.Generate(lineages...))

	// The schema jenny, run on its own, is unchanged.
	printLineageFiles(sch.New("GoTypes", schemaGoTypes).Generate(lineages[0].Schemas[0]))
	// Output:
	// == playlist/v0_0/types.go (GoTypes)
	// package v0_0
	//
	// type Spec struct {
	// 	Name string
	// }
	// == playlist/v1_0/types.go (GoTypes)
	// package v1_0
	//
	// type Spec struct {
	// 	Name     string
	// 	Interval string
	// }
	// == types.go (GoTypes)
	// package schema
	//
	// type Spec struct {
	// 	Name string
	// }
}

// placedSchema is a Schema with the directory its output goes in and the Go
// package it's in.
type placedSchema struct {
	Schema   Schema
	Dir, Pkg string
}

func placeSchemas(l Lineage) ([]placedSchema, error) {
	ps := make([]placedSchema, 0, len(l.Schemas))
	for _, s := range l.Schemas {
		v := versionDir(s)
		ps = append(ps, placedSchema{Schema: s, Dir: path.Join(l.Name, v), Pkg: v})
	}
	return ps, nil
}

// placedGoTypes generates Go types for a placed schema, and writes its own
// path and package from the placement.
func placedGoTypes(p placedSchema) (jenny.File, error) {
	src, err := goSpec(p.Pkg, p.Schema)
	if err != nil {
		return nil, err
	}
	return jenny.NewFile(path.Join(p.Dir, "types.go"), src), nil
}

// The alternative to Example_lineagePrefixBy: the adapter decides the layout,
// as data, and the schema jenny writes its own path and package from it. The
// output is the same.
func Example_lineageAdapt() {
	lin := jenny.For[Lineage](jenny.InputName(func(l Lineage) string { return l.Name }))
	placed := jenny.For[placedSchema](jenny.InputName(func(p placedSchema) string { return p.Dir }))

	gen := lin.Adapt(placeSchemas, placed.New("GoTypes", placedGoTypes))

	printLineageFiles(gen.Generate(lineages...))
	// Output:
	// == playlist/v0_0/types.go (GoTypes)
	// package v0_0
	//
	// type Spec struct {
	// 	Name string
	// }
	// == playlist/v1_0/types.go (GoTypes)
	// package v1_0
	//
	// type Spec struct {
	// 	Name     string
	// 	Interval string
	// }
}

// goPackageFromDir names the package from wherever the file ends up, so it can
// sit outside any later Prefix, and fails if it runs before the file is placed.
func TestGoPackageFromDir(t *testing.T) {
	sch := jenny.For[Schema](jenny.InputName(versionDir))
	gen := sch.New("GoTypes", schemaGoTypes)
	s := lineages[0].Schemas[1]

	outer := sch.PrefixBy(versionDir, gen).Map(goPackageFromDir).Prefix("pkg/playlist")
	fl := mustGen(t, outer, s)
	if got, want := fl[0].Path(), "pkg/playlist/v1_0/types.go"; got != want {
		t.Errorf("path is %q, want %q", got, want)
	}
	if data, _ := jenny.BytesOf(fl[0]); !bytes.HasPrefix(data, []byte("package v1_0\n")) {
		t.Errorf("package clause not rewritten:\n%s", data)
	}

	if _, err := sch.PrefixBy(versionDir, gen.Map(goPackageFromDir)).Generate(s); err == nil || !strings.Contains(err.Error(), `directory "." is not a Go package name`) {
		t.Errorf("expected an error for a file with no directory, got %v", err)
	}
	if _, err := sch.PrefixBy(func(Schema) string { return "v1.0" }, gen).Map(goPackageFromDir).Generate(s); err == nil || !strings.Contains(err.Error(), `directory "v1.0" is not a Go package name`) {
		t.Errorf("expected an error for a directory that isn't a Go identifier, got %v", err)
	}
}
