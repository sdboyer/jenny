package jenny_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sdboyer/jenny"
)

// thisLine returns the line it's called on.
func thisLine() int {
	pc := make([]uintptr, 1)
	f, _ := runtime.CallersFrames(pc[:runtime.Callers(2, pc)]).Next()
	return f.Line
}

// groupTypes adapts a Group to the Types in it.
func groupTypes(g Group) ([]Type, error) { return g.Types, nil }

func allTypes(gs ...Group) ([]Type, error) {
	var ts []Type
	for _, g := range gs {
		ts = append(ts, g.Types...)
	}
	return ts, nil
}

// typeIndex lists the names of the types it's given.
func typeIndex(ts ...Type) (jenny.File, error) {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	return jenny.NewFile("index.txt", []byte(strings.Join(names, ","))), nil
}

func typeFiles(t Type) (jenny.Files, error) {
	return jenny.Files{jenny.NewFile(t.Name+".go", nil), jenny.NewFile(t.Name+"_test.go", nil)}, nil
}

func typeBundle(...Type) (jenny.Files, error) { return nil, nil }

func resourceDir(r Resource) string { return r.Name }

// inTests moves test files into a tests directory.
func inTests(j jenny.Jenny[Type]) jenny.Jenny[Type] { return j.Map(testsDir) }

func testsDir(f jenny.File) (jenny.File, error) {
	if strings.HasSuffix(f.Path(), "_test.go") {
		return jenny.Move(f, "tests/"+f.Path()), nil
	}
	return f, nil
}

func stamp(f jenny.File) (jenny.File, error) { return f, nil }

// typeGens holds generator funcs as methods.
type typeGens struct{}

// Docs writes a type's documentation.
func (typeGens) Docs(t Type) (jenny.File, error) { return jenny.NewFile(t.Name+".md", nil), nil }

// typeCopy copies its input, whatever its type.
func typeCopy[T any](T) (jenny.File, error) { return nil, nil }

// box holds a value.
type box[T any] struct{ Value T }

func mermaid[I any](j jenny.Jenny[I], opts ...jenny.DiagramOption) func() (string, error) {
	return func() (string, error) { return j.Mermaid(opts...) }
}

//nolint:dupword // the expected diagrams name nodes more than once in a row
func TestMermaid(t *testing.T) {
	group, typ := jenny.For[Group](), jenny.For[Type]()
	index := typ.New("Index", typeIndex)
	files := typ.New("Files", typeFiles)

	lin := jenny.For[Lineage](jenny.InputName(func(l Lineage) string { return l.Name }))
	sch := jenny.For[Schema](jenny.InputName(versionDir))
	placed := jenny.For[placedSchema](jenny.InputName(func(p placedSchema) string { return p.Dir }))
	var zero jenny.Jenny[Type]

	// A type declared in a func has no doc to find.
	type unlisted struct{}
	lineageName, lineageNameLine := func(l Lineage) string { return l.Name }, thisLine()
	toUnlisted, toUnlistedLine := func(Group) (unlisted, error) { return unlisted{}, nil }, thisLine()

	for _, tc := range []struct {
		name string
		got  func() (string, error)
		want string
	}{
		{
			// The adapter's shape decides how often the Adapt runs the jennies
			// in it, and so how many files a many-input jenny makes.
			name: "adapt fan-out",
			got:  mermaid(jenny.Pipeline(group.Adapt(groupTypes, index), group.Adapt(allTypes, index))),
			want: `flowchart LR
    n0(["` + "`Group`" + `"])
    subgraph n1["` + "`**Adapt**`" + `"]
        n2[["` + "`groupTypes`" + `"]]
        n3(["` + "`Type`" + `"])
        n4["` + "`**Index** · typeIndex\n./…\n1 file per Group`" + `"]
    end
    subgraph n5["` + "`**Adapt**`" + `"]
        n6[["` + "`allTypes`" + `"]]
        n7(["` + "`Type`" + `"])
        n8["` + "`**Index** · typeIndex\n./…\n1 file`" + `"]
    end
    n0 -->|"1→N"| n2
    n2 --> n3
    n3 -->|"N→1"| n4
    n0 -->|"N→N"| n6
    n6 --> n7
    n7 -->|"N→1"| n8
`,
		},
		{
			// A many-input jenny inside a PrefixBy is run once per input.
			name: "PrefixBy",
			got:  mermaid(api.PrefixBy(resourceDir, api.New("Routes", routes)).Prefix("web")),
			want: `flowchart LR
    n0(["` + "`Resource`" + `"])
    subgraph n1["` + "`**PrefixBy** resourceDir → **Prefix** web`" + `"]
        n2["` + "`**Routes** · routes\nweb/{Resource}/…\n1 file per Resource`" + `"]
    end
    n0 -->|"N→1"| n2
`,
		},
		{
			name: "With",
			got:  mermaid(files.With(inTests).Prefix("gen").Map(stamp)),
			want: `flowchart LR
    n0(["` + "`Type`" + `"])
    subgraph n1["` + "`**With** inTests → **Prefix** gen → **Map** stamp`" + `"]
        n2["` + "`**Files** · typeFiles\n**Map** testsDir\ngen/…\nN files per Type`" + `"]
    end
    n0 -->|"1→N"| n2
`,
		},
		{
			// Pipelines that only group jennies are drawn as the jennies they
			// run, and decorators applied to one jenny are drawn on it.
			name: "pipelines",
			got: mermaid(jenny.Pipeline(
				jenny.Pipeline(index, files),
				jenny.Prefix("one", index),
				jenny.Pipeline[Type](),
				zero.Prefix("empty"),
				jenny.Prefix("both", index, files).Prefix("outer"),
			)),
			want: `flowchart LR
    n0(["` + "`Type`" + `"])
    n1["` + "`**Index** · typeIndex\n./…\n1 file`" + `"]
    n2["` + "`**Files** · typeFiles\n./…\nN files per Type`" + `"]
    n3["` + "`**Index** · typeIndex\n**Prefix** one\none/…\n1 file`" + `"]
    n4["` + "`(empty)\n**Prefix** empty`" + `"]
    subgraph n5["` + "`**Prefix** both → **Prefix** outer`" + `"]
        n6["` + "`**Index** · typeIndex\nouter/both/…\n1 file`" + `"]
        n7["` + "`**Files** · typeFiles\nouter/both/…\nN files per Type`" + `"]
    end
    n0 -->|"N→1"| n1
    n0 -->|"1→N"| n2
    n0 -->|"N→1"| n3
    n0 --> n4
    n0 -->|"N→1"| n6
    n0 -->|"1→N"| n7
`,
		},
		{
			name: "zero Jenny",
			got:  mermaid(zero),
			want: `flowchart LR
    n0(["` + "`Type`" + `"])
    n1["` + "`(empty)`" + `"]
    n0 --> n1
`,
		},
		{
			name: "many outputs",
			got:  mermaid(jenny.Pipeline(files, typ.New("Bundle", typeBundle))),
			want: `flowchart LR
    n0(["` + "`Type`" + `"])
    n1["` + "`**Files** · typeFiles\n./…\nN files per Type`" + `"]
    n2["` + "`**Bundle** · typeBundle\n./…\nN files`" + `"]
    n0 -->|"1→N"| n1
    n0 -->|"N→N"| n2
`,
		},
		{
			// Characters Mermaid or markdown would read as markup are escaped,
			// and type arguments are written without their import paths.
			name: "escaping",
			got:  mermaid(jenny.For[box[Field]]().New("a_b*c\"d<e>#f&g`h~i\\j", typeCopy[box[Field]]).Prefix("_x_")),
			want: `flowchart LR
    n0(["` + "`box[jenny\\_test.Field]`" + `"])
    n1["` + "`**a\\_b\\*c#quot;d#lt;e#gt;#35;f#amp;g'h\\~i\\\\j** · typeCopy\n**Prefix** \\_x\\_\n\\_x\\_/…\n1 file per box[jenny\\_test.Field]`" + `"]
    n0 -->|"1→1"| n1
`,
		},
		{
			name: "lineage, PrefixBy",
			got: mermaid(lin.PrefixBy(lineageName,
				lin.Adapt(schemasOf,
					sch.PrefixBy(versionDir, sch.New("GoTypes", schemaGoTypes)),
				),
			).Map(goPackageFromDir), jenny.DiagramDocs()),
			want: `flowchart LR
    n0(["` + "`Lineage`" + `"])
    subgraph n1["` + "`**PrefixBy** func@mermaid\\_test.go:" + fmt.Sprint(lineageNameLine) + " → **Map** goPackageFromDir`" + `"]
        subgraph n2["` + "`**Adapt**`" + `"]
            n3[["` + "`schemasOf`" + `"]]
            n4(["` + "`Schema`" + `"])
            subgraph n5["` + "`**PrefixBy** versionDir`" + `"]
                n6["` + "`**GoTypes** · schemaGoTypes\n{Lineage}/{Schema}/…\n1 file per Schema`" + `"]
            end
        end
    end
    n0 -->|"1→N"| n3
    n3 --> n4
    n4 -->|"1→1"| n6
    click n0 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#Lineage" "Lineage and Schema stand in for a thema lineage, which holds several versions of a schema."
    click n6 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#schemaGoTypes" "schemaGoTypes generates Go types for one schema. It knows nothing about lineages: it writes types.go in package schema, so it's also usable on its own over a single schema."
`,
		},
		{
			name: "lineage, Adapt",
			got:  mermaid(lin.Adapt(placeSchemas, placed.New("GoTypes", placedGoTypes)), jenny.DiagramDocs()),
			want: `flowchart LR
    n0(["` + "`Lineage`" + `"])
    subgraph n1["` + "`**Adapt**`" + `"]
        n2[["` + "`placeSchemas`" + `"]]
        n3(["` + "`placedSchema`" + `"])
        n4["` + "`**GoTypes** · placedGoTypes\n./…\n1 file per placedSchema`" + `"]
    end
    n0 -->|"1→N"| n2
    n2 --> n3
    n3 -->|"1→1"| n4
    click n0 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#Lineage" "Lineage and Schema stand in for a thema lineage, which holds several versions of a schema."
    click n3 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#placedSchema" "placedSchema is a Schema with the directory its output goes in and the Go package it's in."
    click n4 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#placedGoTypes" "placedGoTypes generates Go types for a placed schema, and writes its own path and package from the placement."
`,
		},
		{
			// Methods and generic funcs are found by their declared names, and
			// a type declared inside a func has no tooltip.
			name: "docs",
			got: mermaid(jenny.Pipeline(
				group.Adapt(groupTypes,
					typ.New("Docs", typeGens{}.Docs),
					typ.New("Copy", typeCopy[Type]),
					index.Map(stamp),
				),
				jenny.Prefix("unlisted", group.Adapt[unlisted](toUnlisted)),
			), jenny.DiagramDocs()),
			want: `flowchart LR
    n0(["` + "`Group`" + `"])
    subgraph n1["` + "`**Adapt**`" + `"]
        n2[["` + "`groupTypes`" + `"]]
        n3(["` + "`Type`" + `"])
        n4["` + "`**Docs** · typeGens.Docs\n./…\n1 file per Type`" + `"]
        n5["` + "`**Copy** · typeCopy\n./…\n1 file per Type`" + `"]
        n6["` + "`**Index** · typeIndex\n**Map** stamp\n./…\n1 file per Group`" + `"]
    end
    subgraph n7["` + "`**Adapt** → **Prefix** unlisted`" + `"]
        n8[["` + "`func@mermaid\\_test.go:" + fmt.Sprint(toUnlistedLine) + "`" + `"]]
        n9(["` + "`unlisted`" + `"])
        n10["` + "`(empty)`" + `"]
    end
    n0 -->|"1→N"| n2
    n2 --> n3
    n3 -->|"1→1"| n4
    n3 -->|"1→1"| n5
    n3 -->|"N→1"| n6
    n0 -->|"1→1"| n8
    n8 --> n9
    n9 --> n10
    click n2 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#groupTypes" "groupTypes adapts a Group to the Types in it."
    click n4 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#typeGens.Docs" "Docs writes a type's documentation."
    click n5 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#typeCopy" "typeCopy copies its input, whatever its type."
    click n6 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#typeIndex" "typeIndex lists the names of the types it's given."
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.got()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("diagram mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A FileMapper that isn't a named func is labeled with where Map was called,
// or given DiagramDocs, with the expression Map was called with.
func TestMermaidMapLabels(t *testing.T) {
	index := jenny.For[Type]().New("Index", typeIndex)
	method, methodLine := index.Map(jenny.BytesMapper(func(_ jenny.File, b []byte) ([]byte, error) { return b, nil })), thisLine()
	function, functionLine := jenny.Map(gofmt, index), thisLine()
	literal, literalLine := index.Map(func(f jenny.File) (jenny.File, error) { return f, nil }), thisLine()

	for _, tc := range []struct {
		name  string
		j     jenny.Jenny[Type]
		opts  []jenny.DiagramOption
		label string
	}{
		{"method", method, nil, fmt.Sprintf("@mermaid\\_test.go:%d", methodLine)},
		{"func", function, nil, fmt.Sprintf("@mermaid\\_test.go:%d", functionLine)},
		{"method with docs", method, []jenny.DiagramOption{jenny.DiagramDocs()}, "jenny.BytesMapper(…)"},
		{"func with docs", function, []jenny.DiagramOption{jenny.DiagramDocs()}, "gofmt"},
		{"literal with docs", literal, []jenny.DiagramOption{jenny.DiagramDocs()}, fmt.Sprintf("func@mermaid\\_test.go:%d", literalLine)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.j.Mermaid(tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if want := "**Map** " + tc.label + "\n"; !strings.Contains(got, want) {
				t.Errorf("no %q in\n%s", want, got)
			}
		})
	}
}

func TestMermaidDocsErrors(t *testing.T) {
	index := jenny.For[Type]().New("Index", typeIndex)

	// Two FileMappers given to Map on one line can't be told apart.
	twoMaps, line := jenny.Map(header, index.Map(gofmt)), thisLine()
	_, err := twoMaps.Mermaid(jenny.DiagramDocs())
	if want := fmt.Sprintf("more than one call to Map at mermaid_test.go:%d", line); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("got error %v, want one saying %q", err, want)
	}
	if _, err := twoMaps.Mermaid(); err != nil {
		t.Errorf("without DiagramDocs, got error %v", err)
	}

	// Outside the module, the source of its packages can't be found.
	t.Chdir(t.TempDir())
	_, err = index.Mermaid(jenny.DiagramDocs())
	if want := "Mermaid: reading the source of package github.com/sdboyer/jenny_test: "; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("got error %v, want one starting %q", err, want)
	}
}

// A generator is often a main package, whose types and funcs the runtime
// names as package main. This builds one against this checkout of jenny and
// runs it.
func TestMermaidDocsMain(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generator")
	}
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile("go.sum")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string]string{
		"go.mod": "module example.com/gen\n\ngo 1.27.0\n\nrequire github.com/sdboyer/jenny v0.0.0\n\nreplace github.com/sdboyer/jenny => " + filepath.ToSlash(repo) + "\n",
		"go.sum": string(sum),
		"main.go": `package main

import (
	"fmt"

	"github.com/sdboyer/jenny"
)

// Kind is a kind of thing to generate code for.
type Kind struct{ Name string }

// goTypes writes a kind's Go types.
func goTypes(k Kind) (jenny.File, error) { return jenny.NewFile(k.Name+".go", nil), nil }

func main() {
	fmt.Print(jenny.For[Kind]().New("GoTypes", goTypes).Mermaid(jenny.DiagramDocs()))
}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.CommandContext(t.Context(), "go", "run", ".")
	cmd.Dir = dir
	// Everything the generator needs is in the module cache already, from
	// building this test.
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	for _, want := range []string{
		`click n0 href "https://pkg.go.dev/example.com/gen#Kind" "Kind is a kind of thing to generate code for."`,
		`click n1 href "https://pkg.go.dev/example.com/gen#goTypes" "goTypes writes a kind's Go types."`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}
