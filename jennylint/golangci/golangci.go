// Package golangci registers the [jennylint] analyzer as a golangci-lint
// module plugin named "jennylint".
//
// To use it, build a custom golangci-lint with this in .custom-gcl.yml:
//
//	version: v2.12.2 # your golangci-lint version
//	plugins:
//	  - module: github.com/sdboyer/jenny/jennylint
//	    import: github.com/sdboyer/jenny/jennylint/golangci
//	    version: latest
//
// and enable it in .golangci.yml:
//
//	linters:
//	  enable:
//	    - jennylint
//	  settings:
//	    custom:
//	      jennylint:
//	        type: module
//	        description: checks for nil comparisons, type switches and type assertions on a jenny.File
//
// Then run golangci-lint custom, which builds ./custom-gcl, and run
// ./custom-gcl run in place of golangci-lint run.
package golangci

import (
	"github.com/golangci/plugin-module-register/register"
	"github.com/sdboyer/jenny/jennylint"
	"golang.org/x/tools/go/analysis"
)

func init() { //nolint:gochecknoinits // golangci-lint finds module plugins by their registering in init
	register.Plugin("jennylint", New)
}

// New returns the jennylint plugin. It takes no settings.
func New(any) (register.LinterPlugin, error) {
	return plugin{}, nil
}

type plugin struct{}

func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{jennylint.Analyzer}, nil
}

func (plugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
