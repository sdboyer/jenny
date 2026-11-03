// The jennylint command runs the [jennylint] analyzer, standalone or as a vet
// tool:
//
//	jennylint ./...
//	go vet -vettool=$(which jennylint) ./...
package main

import (
	"github.com/sdboyer/jenny/jennylint"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(jennylint.Analyzer) }
