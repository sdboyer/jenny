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

// boxType puts a Type in a box.
func boxType(t Type) (box[Type], error) { return box[Type]{t}, nil }

// typeFields adapts a Type to its fields, of which a Type in these tests has
// none.
func typeFields(Type) ([]Field, error) { return nil, nil }

func allFields(...Type) ([]Field, error) { return nil, nil }

func fieldFiles(Field) (jenny.Files, error) { return nil, nil }

func fieldIndex(...Field) (jenny.File, error) { return nil, nil }

func mermaid[I any](j jenny.Jenny[I], opts ...jenny.DiagramOption) func() (string, error) {
	return func() (string, error) { return j.Mermaid(opts...) }
}

// expectedDiagram returns want with each ˋ replaced by a backtick, which a raw
// string can't hold.
func expectedDiagram(want string) string { return strings.ReplaceAll(want, "ˋ", "`") }

//nolint:dupword,maintidx // a table of expected diagrams, which name nodes more than once in a row
func TestMermaid(t *testing.T) {
	group, typ, field := jenny.For[Group](), jenny.For[Type](), jenny.For[Field]()
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
			// A single-input adapter runs the jennies in it once per input, so
			// a many-input jenny in it makes a file per input. Each func that
			// can return many multiplies the count by a factor of its own.
			name: "counts",
			got: mermaid(jenny.Pipeline(
				group.Adapt(groupTypes,
					files,
					typ.Adapt(typeFields, field.New("FieldFiles", fieldFiles)),
					typ.Adapt(allFields, field.New("FieldIndex", fieldIndex)),
				),
				group.Adapt(allTypes, index),
			)),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Group** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        subgraph n2["ˋ ˋ"]
            n3(["ˋ**Type**
*groupTypes* ×k₁ˋ"])
            n4["ˋ**Files** ×k₂ˋ"]
            subgraph n6["ˋ ˋ"]
                n7(["ˋ**Field**
*typeFields* ×k₃ˋ"])
                n8["ˋ**FieldFiles** ×k₄ˋ"]
            end
            n9(["ˋ**Field**
*allFields* ×k₅ˋ"])
            n10["ˋ**FieldIndex**ˋ"]
        end
        n11(["ˋ**Type**
*allTypes* ×k₆ˋ"])
        n12["ˋ**Index**ˋ"]
    end
    n5["ˋ./ˋ"]@{ shape: folder }
    n0 --> n3
    n3 --> n4
    n4 -.-|"n·k₁·k₂"| n5
    n3 --> n7
    n7 --> n8
    n8 -.-|"n·k₁·k₃·k₄"| n5
    n3 ==> n9
    n9 ==> n10
    n10 -.-|"n"| n5
    n0 ==> n11
    n11 ==> n12
    n12 -.-|"1"| n5
    classDef hidden fill:none,stroke:none
    class n1 hidden
    classDef each fill:none,stroke-dasharray:4 3
    class n2,n6 each
`,
		},
		{
			// A many-input jenny inside a PrefixBy is run once per input.
			name: "PrefixBy",
			got:  mermaid(api.PrefixBy(resourceDir, api.New("Routes", routes)).Prefix("web")),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Resource** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        subgraph n2["ˋ**PrefixBy** resourceDirˋ"]
            n3["ˋ**Routes**ˋ"]
        end
    end
    n4["ˋweb/{Resource}/ˋ"]@{ shape: folder }
    n0 ==> n3
    n3 -.-|"n"| n4
    classDef hidden fill:none,stroke:none
    class n1 hidden
    classDef each fill:none,stroke-dasharray:4 3
    class n2 each
`,
		},
		{
			name: "With",
			got:  mermaid(files.With(inTests).Prefix("gen").Map(stamp)),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Type** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        subgraph n2["ˋ**Map** stampˋ"]
            subgraph n3["ˋ**With** inTestsˋ"]
                subgraph n4["ˋ**Map** testsDirˋ"]
                    n5["ˋ**Files** ×kˋ"]
                end
            end
        end
    end
    n6["ˋgen/ˋ"]@{ shape: folder }
    n0 --> n5
    n5 -.-|"n·k"| n6
    classDef hidden fill:none,stroke:none
    class n1 hidden
`,
		},
		{
			// Pipelines are drawn as the jennies they run, and a Prefix only
			// as the directory files are placed in. A jenny used twice has the
			// same factor each time.
			name: "pipelines",
			got: mermaid(jenny.Pipeline(
				jenny.Pipeline(index, files),
				jenny.Prefix("one", index),
				jenny.Pipeline[Type](),
				zero.Prefix("empty"),
				jenny.Prefix("both", index, files).Prefix("outer"),
			)),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Type** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        n2["ˋ**Index**ˋ"]
        n4["ˋ**Files** ×kˋ"]
        n5["ˋ**Index**ˋ"]
        n7["ˋ(empty)ˋ"]
        n8["ˋ**Index**ˋ"]
        n10["ˋ**Files** ×kˋ"]
    end
    n3["ˋ./ˋ"]@{ shape: folder }
    n6["ˋone/ˋ"]@{ shape: folder }
    n9["ˋouter/both/ˋ"]@{ shape: folder }
    n0 ==> n2
    n2 -.-|"1"| n3
    n0 --> n4
    n4 -.-|"n·k"| n3
    n0 ==> n5
    n5 -.-|"1"| n6
    n0 --> n7
    n0 ==> n8
    n8 -.-|"1"| n9
    n0 --> n10
    n10 -.-|"n·k"| n9
    classDef hidden fill:none,stroke:none
    class n1 hidden
`,
		},
		{
			name: "zero Jenny",
			got:  mermaid(zero),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Type** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        n2["ˋ(empty)ˋ"]
    end
    n0 --> n2
    classDef hidden fill:none,stroke:none
    class n1 hidden
`,
		},
		{
			// A many-input jenny that returns Files makes some number of files
			// in all.
			name: "many outputs",
			got:  mermaid(jenny.Pipeline(files, typ.New("Bundle", typeBundle))),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Type** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        n2["ˋ**Files** ×k₁ˋ"]
        n4["ˋ**Bundle** ×k₂ˋ"]
    end
    n3["ˋ./ˋ"]@{ shape: folder }
    n0 --> n2
    n2 -.-|"n·k₁"| n3
    n0 ==> n4
    n4 -.-|"k₂"| n3
    classDef hidden fill:none,stroke:none
    class n1 hidden
`,
		},
		{
			// Characters Mermaid or markdown would read as markup are escaped,
			// and type arguments are written without their import paths.
			name: "escaping",
			got:  mermaid(jenny.For[box[Field]]().New("a_b*c\"d<e>#f&g`h~i\\j", typeCopy[box[Field]]).Prefix("_x_")),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**box[jenny\_test.Field]** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        n2["ˋ**a\_b\*c#quot;d#lt;e#gt;#35;f#amp;g'h\~i\\j**ˋ"]
    end
    n3["ˋ\_x\_/ˋ"]@{ shape: folder }
    n0 --> n2
    n2 -.-|"n"| n3
    classDef hidden fill:none,stroke:none
    class n1 hidden
`,
		},
		{
			name: "lineage, PrefixBy",
			got: mermaid(lin.PrefixBy(lineageName,
				lin.Adapt(schemasOf,
					sch.PrefixBy(versionDir, sch.New("GoTypes", schemaGoTypes)),
				),
			).Map(goPackageFromDir), jenny.DiagramDocs()),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Lineage** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        subgraph n2["ˋ**Map** goPackageFromDirˋ"]
            subgraph n3["ˋ**PrefixBy** func@mermaid\_test.go:` + fmt.Sprint(lineageNameLine) + `ˋ"]
                subgraph n4["ˋ ˋ"]
                    n5(["ˋ**Schema**
*schemasOf* ×kˋ"])
                    subgraph n6["ˋ**PrefixBy** versionDirˋ"]
                        n7["ˋ**GoTypes**ˋ"]
                    end
                end
            end
        end
    end
    n8["ˋ{Lineage}/{Schema}/ˋ"]@{ shape: folder }
    n0 --> n5
    n5 --> n7
    n7 -.-|"n·k"| n8
    click n0 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#Lineage" "Lineage and Schema stand in for a thema lineage, which holds several versions of a schema."
    click n7 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#schemaGoTypes" "schemaGoTypes generates Go types for one schema. It knows nothing about lineages: it writes types.go in package schema, so it's also usable on its own over a single schema."
    classDef hidden fill:none,stroke:none
    class n1 hidden
    classDef each fill:none,stroke-dasharray:4 3
    class n3,n4,n6 each
`,
		},
		{
			name: "lineage, Adapt",
			got:  mermaid(lin.Adapt(placeSchemas, placed.New("GoTypes", placedGoTypes)), jenny.DiagramDocs()),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Lineage** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        subgraph n2["ˋ ˋ"]
            n3(["ˋ**placedSchema**
*placeSchemas* ×kˋ"])
            n4["ˋ**GoTypes**ˋ"]
        end
    end
    n5["ˋ./ˋ"]@{ shape: folder }
    n0 --> n3
    n3 --> n4
    n4 -.-|"n·k"| n5
    click n0 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#Lineage" "Lineage and Schema stand in for a thema lineage, which holds several versions of a schema."
    click n3 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#placedSchema" "placedSchema is a Schema with the directory its output goes in and the Go package it's in."
    click n4 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#placedGoTypes" "placedGoTypes generates Go types for a placed schema, and writes its own path and package from the placement."
    classDef hidden fill:none,stroke:none
    class n1 hidden
    classDef each fill:none,stroke-dasharray:4 3
    class n2 each
`,
		},
		{
			// Methods and generic funcs are found by their declared names, and
			// a type declared inside a func has no tooltip. An Adapt's node has
			// the docs of both its type and its adapter, and links to the
			// type's, if it has one.
			name: "docs",
			got: mermaid(jenny.Pipeline(
				group.Adapt(groupTypes,
					typ.New("Docs", typeGens{}.Docs),
					typ.New("Copy", typeCopy[Type]),
					index.Map(stamp),
					typ.Adapt(boxType, jenny.For[box[Type]]().New("Boxed", typeCopy[box[Type]])),
				),
				jenny.Prefix("unlisted", group.Adapt[unlisted](toUnlisted)),
			), jenny.DiagramDocs()),
			want: `---
config:
  layout: elk
---
flowchart LR
    n0(["ˋ**Group** ×nˋ"])
    subgraph n1["ˋ ˋ"]
        subgraph n2["ˋ ˋ"]
            n3(["ˋ**Type**
*groupTypes* ×kˋ"])
            n4["ˋ**Docs**ˋ"]
            n6["ˋ**Copy**ˋ"]
            subgraph n7["ˋ**Map** stampˋ"]
                n8["ˋ**Index**ˋ"]
            end
            subgraph n9["ˋ ˋ"]
                n10(["ˋ**box[jenny\_test.Type]**
*boxType*ˋ"])
                n11["ˋ**Boxed**ˋ"]
            end
        end
        subgraph n12["ˋ ˋ"]
            n13(["ˋ**unlisted**
*func@mermaid\_test.go:` + fmt.Sprint(toUnlistedLine) + `*ˋ"])
            n14["ˋ(empty)ˋ"]
        end
    end
    n5["ˋ./ˋ"]@{ shape: folder }
    n0 --> n3
    n3 --> n4
    n4 -.-|"n·k"| n5
    n3 --> n6
    n6 -.-|"n·k"| n5
    n3 ==> n8
    n8 -.-|"n"| n5
    n3 --> n10
    n10 --> n11
    n11 -.-|"n·k"| n5
    n0 --> n13
    n13 --> n14
    click n3 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#groupTypes" "groupTypes adapts a Group to the Types in it."
    click n4 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#typeGens.Docs" "Docs writes a type's documentation."
    click n6 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#typeCopy" "typeCopy copies its input, whatever its type."
    click n8 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#typeIndex" "typeIndex lists the names of the types it's given."
    click n10 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#box" "box holds a value. boxType puts a Type in a box."
    click n11 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#typeCopy" "typeCopy copies its input, whatever its type."
    classDef hidden fill:none,stroke:none
    class n1 hidden
    classDef each fill:none,stroke-dasharray:4 3
    class n2,n9,n12 each
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.got()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(expectedDiagram(tc.want), got); diff != "" {
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
			if want := "**Map** " + tc.label + "`"; !strings.Contains(got, want) {
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
		`click n2 href "https://pkg.go.dev/example.com/gen#goTypes" "goTypes writes a kind's Go types."`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}
