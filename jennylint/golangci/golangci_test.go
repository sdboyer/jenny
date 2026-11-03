package golangci_test

import (
	"testing"

	"github.com/golangci/plugin-module-register/register"
	"github.com/sdboyer/jenny/jennylint"
	_ "github.com/sdboyer/jenny/jennylint/golangci"
)

func TestPluginRegistered(t *testing.T) {
	newPlugin, err := register.GetPlugin("jennylint")
	if err != nil {
		t.Fatal(err)
	}
	p, err := newPlugin(nil)
	if err != nil {
		t.Fatal(err)
	}
	as, err := p.BuildAnalyzers()
	if err != nil {
		t.Fatal(err)
	}
	if len(as) != 1 || as[0] != jennylint.Analyzer {
		t.Errorf("BuildAnalyzers = %v, want jennylint.Analyzer", as)
	}
	if got := p.GetLoadMode(); got != register.LoadModeTypesInfo {
		t.Errorf("GetLoadMode = %q, want %q", got, register.LoadModeTypesInfo)
	}
}
