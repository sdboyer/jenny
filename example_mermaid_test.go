package jenny_test

import (
	"fmt"

	"github.com/sdboyer/jenny"
)

func genGo(s string) (jenny.File, error) { return jenny.NewFile(s+".go", nil), nil }

func genIndex(...string) (jenny.File, error) { return jenny.NewFile("index.go", nil), nil }

// Mermaid draws how a jenny is composed, from its declaration alone. Here, Go
// makes a file per input, and Index makes one file from all of them, both in
// gen. Paste the output into anything that renders Mermaid.
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
	// flowchart LR
	//     n0(["`string`"])
	//     subgraph n1["`**Prefix** gen`"]
	//         n2["`**Go** · genGo
	// gen/…
	// 1 file per string`"]
	//         n3["`**Index** · genIndex
	// gen/…
	// 1 file`"]
	//     end
	//     n0 -->|"1→1"| n2
	//     n0 -->|"N→1"| n3
}
