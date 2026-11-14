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
	// ---
	// flowchart LR
	//     n0(["`**string** ×n`"])
	//     subgraph n1["` `"]
	//         n2["`**Go**`"]
	//         n5["`**Index**`"]
	//     end
	//     n3["`n`"]@{ shape: doc }
	//     n4["`gen/`"]@{ shape: folder }
	//     n6["`1`"]@{ shape: doc }
	//     n0 --> n2
	//     n2 -.- n3 -.- n4
	//     n0 ==> n5
	//     n5 -.- n6 -.- n4
	//     classDef hidden fill:none,stroke:none
	//     class n1 hidden
}
