package jennylint_test

import (
	"path/filepath"
	"testing"

	"github.com/sdboyer/jenny/jennylint"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	analysistest.RunWithSuggestedFixes(t, dir, jennylint.Analyzer,
		"example.com/user/src/bad",
		"example.com/user/src/good",
		"example.com/user/src/renamed",
		"example.com/user/src/dotimport",
		"example.com/user/src/noimport",
	)
}

// Package jenny itself compares Files with nil and type-asserts them, where
// it turns pointers into values. It isn't checked.
func TestJennyItselfIsExempt(t *testing.T) {
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, dir, jennylint.Analyzer, "github.com/sdboyer/jenny")
}
