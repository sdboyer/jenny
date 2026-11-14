package jenny_test

import (
	"fmt"

	"github.com/sdboyer/jenny"
)

func genGo(s string) (jenny.File, error) { return jenny.NewFile(s+".go", nil), nil }

func genIndex(...string) (jenny.File, error) { return jenny.NewFile("index.go", nil), nil }

// Mermaid draws how a jenny is composed, from its declaration alone. Here, Go
// makes a file per input, n in all, and Index makes one file from all of
// them, both in gen. Paste the output into anything that renders Mermaid.
func ExampleJenny_Mermaid() {
	str := jenny.For[string]()
	gen := jenny.Prefix("gen",
		str.New("Go", genGo),
		str.New("Index", genIndex),
	)

	diagram, err := gen.Mermaid()
	if err != nil {
		panic(err)
	}
	fmt.Print(diagram)
	// Output:
	// ---
	// config:
	//   layout: elk
	//   themeCSS: "span.edgeLabel { display: inline-block; padding: 3px 8px; }"
	// ---
	// flowchart LR
	//     n0(["`**string** ×n`"])
	//     subgraph n1["` `"]
	//         n2["`**Go**`"]
	//         n4["`**Index**`"]
	//     end
	//     n3["`gen/`"]@{ shape: folder }
	//     n0 --> n2
	//     n2 -.-|"n"| n3
	//     n0 ==> n4
	//     n4 -.-|"1"| n3
	//     classDef hidden fill:none,stroke:none
	//     class n1 hidden
}
