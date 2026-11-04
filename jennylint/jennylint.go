// Package jennylint defines an analyzer that reports code asking what a
// jenny.File holds in a way that compiles but doesn't work.
//
// A jenny.File is an interface, and *jenny.Bytes and *jenny.Tree[T] satisfy it
// as well as jenny.Bytes and jenny.Tree[T] do. So a File can hold a nil
// pointer, which is not == nil, or a pointer, which a case naming the value
// type doesn't match. Jenny hands out only values, so a case naming a pointer
// type never matches at all. The rule (see jenny.File) is never to compare a
// File with nil, type-switch on one or type-assert one, and to ask with
// jenny.IsNil, jenny.TreeOf and jenny.BytesOf instead. jennylint reports:
//
//   - a type-switch case or type assertion on a jenny.File naming *jenny.Bytes
//     or *jenny.Tree[T]. It never matches: that's a bug.
//   - a type-switch case or type assertion on a jenny.File naming jenny.Bytes
//     or jenny.Tree[T]. It matches the Files jenny hands out, but misses a
//     pointer that came from anywhere else, so it suggests jenny.BytesOf or
//     jenny.TreeOf, which match both.
//   - a jenny.File compared with nil, with == or != or as a type-switch case
//     nil. It misses a nil pointer, so it suggests jenny.IsNil. For == and !=
//     it offers that rewrite as a suggested fix.
//
// Each of the three has its own message and diagnostic category: "pointer",
// "value" and "nil". Code in package jenny itself isn't checked: it's where
// pointers are turned into values, so the rule's premise doesn't hold there.
//
// Run it with go vet:
//
//	go install github.com/sdboyer/jenny/jennylint/cmd/jennylint@latest
//	go vet -vettool=$(which jennylint) ./...
//
// To run it from golangci-lint, see package
// github.com/sdboyer/jenny/jennylint/golangci.
package jennylint

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// jennyPath is the import path of package jenny, whose types the analyzer
// matches by path rather than by importing it, so that it works with any
// version of jenny that has them.
const jennyPath = "github.com/sdboyer/jenny"

// Analyzer reports comparisons, type switches and type assertions on a
// jenny.File that compile but don't work. See the package doc.
var Analyzer = &analysis.Analyzer{ //nolint:gochecknoglobals // go/analysis drivers take analyzers as package-level values
	Name: "jennylint",
	Doc:  "report nil comparisons, type switches and type assertions on a jenny.File, which miss pointers; use jenny.IsNil, jenny.TreeOf and jenny.BytesOf",
	URL:  "https://pkg.go.dev/github.com/sdboyer/jenny/jennylint",
	Run:  run,
}

// run returns no result, since Analyzer has no ResultType.
func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg.Path() != jennyPath {
		for _, file := range pass.Files {
			c := checker{pass: pass, jenny: jennyName(file)}
			ast.Inspect(file, c.visit)
		}
	}
	return nil, nil //nolint:nilnil // no ResultType means a nil result
}

// jennyName returns the name file refers to package jenny by: "" for a dot
// import, and "_" if it doesn't import it, or imports it only for effects.
func jennyName(file *ast.File) string {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err != nil || p != jennyPath {
			continue
		}
		if imp.Name == nil {
			return "jenny"
		}
		if imp.Name.Name == "." {
			return ""
		}
		return imp.Name.Name
	}
	return "_"
}

type checker struct {
	pass  *analysis.Pass
	jenny string
}

func (c checker) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		c.comparison(n)
	case *ast.TypeAssertExpr:
		// A type switch's x.(type) has no Type; the switch is checked case
		// by case.
		if n.Type != nil && c.isFile(n.X) {
			c.kind(n.Type, "type assertion to")
		}
	case *ast.TypeSwitchStmt:
		c.typeSwitch(n)
	}
	return true
}

func (c checker) comparison(e *ast.BinaryExpr) {
	if e.Op != token.EQL && e.Op != token.NEQ {
		return
	}
	file := e.X
	switch {
	case c.isNil(e.Y) && c.isFile(e.X):
	case c.isNil(e.X) && c.isFile(e.Y):
		file = e.Y
	default:
		return
	}
	d := analysis.Diagnostic{
		Pos:      e.Pos(),
		End:      e.End(),
		Category: "nil",
		Message:  "comparing a jenny.File with nil misses a nil pointer such as a nil *jenny.Bytes; use jenny.IsNil",
	}
	if c.jenny != "_" {
		var b bytes.Buffer
		if e.Op == token.NEQ {
			b.WriteString("!")
		}
		if c.jenny != "" {
			b.WriteString(c.jenny + ".")
		}
		b.WriteString("IsNil(")
		if err := format.Node(&b, c.pass.Fset, file); err != nil {
			panic(err) // file is a valid expression from the parser
		}
		b.WriteString(")")
		d.SuggestedFixes = []analysis.SuggestedFix{{
			Message:   "use jenny.IsNil",
			TextEdits: []analysis.TextEdit{{Pos: e.Pos(), End: e.End(), NewText: b.Bytes()}},
		}}
	}
	c.pass.Report(d)
}

func (c checker) typeSwitch(s *ast.TypeSwitchStmt) {
	var x ast.Expr
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		x = a.X.(*ast.TypeAssertExpr).X
	case *ast.AssignStmt:
		x = a.Rhs[0].(*ast.TypeAssertExpr).X
	}
	if !c.isFile(x) {
		return
	}
	for _, stmt := range s.Body.List {
		for _, e := range stmt.(*ast.CaseClause).List {
			if c.isNil(e) {
				c.pass.Report(analysis.Diagnostic{
					Pos:      e.Pos(),
					End:      e.End(),
					Category: "nil",
					Message:  "case nil in a type switch on a jenny.File misses a nil pointer such as a nil *jenny.Bytes; use jenny.IsNil",
				})
				continue
			}
			c.kind(e, "case")
		}
	}
}

// kind reports e, a type in a type-switch case or a type assertion on a
// jenny.File, if it names one of jenny's kinds or a pointer to one.
func (c checker) kind(e ast.Expr, what string) {
	t := c.pass.TypesInfo.TypeOf(e)
	ptr, isPtr := types.Unalias(t).(*types.Pointer)
	if isPtr {
		t = ptr.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || !inJenny(n.Obj()) {
		return
	}
	var helper string
	switch n.Origin().Obj().Name() {
	case "Bytes":
		helper = "jenny.BytesOf"
	case "Tree":
		helper = "jenny.TreeOf[" + types.TypeString(n.TypeArgs().At(0), c.qualifier) + "]"
	default:
		return
	}
	name := types.TypeString(t, c.qualifier)
	if isPtr {
		c.pass.Report(analysis.Diagnostic{
			Pos:      e.Pos(),
			End:      e.End(),
			Category: "pointer",
			Message:  what + " *" + name + " on a jenny.File never matches: jenny hands out a " + name + ", never a pointer to one; use " + helper,
		})
		return
	}
	c.pass.Report(analysis.Diagnostic{
		Pos:      e.Pos(),
		End:      e.End(),
		Category: "value",
		Message:  what + " " + name + " on a jenny.File misses a *" + name + "; use " + helper + ", which matches both",
	})
}

// qualifier names package jenny "jenny" in messages, whatever the file calls
// it, so that they match the helper names the messages suggest.
func (c checker) qualifier(p *types.Package) string {
	if p == c.pass.Pkg {
		return ""
	}
	return p.Name()
}

func (c checker) isFile(e ast.Expr) bool {
	n, ok := types.Unalias(c.pass.TypesInfo.TypeOf(e)).(*types.Named)
	return ok && inJenny(n.Obj()) && n.Obj().Name() == "File"
}

func (c checker) isNil(e ast.Expr) bool {
	tv, ok := c.pass.TypesInfo.Types[e]
	return ok && tv.IsNil()
}

func inJenny(obj types.Object) bool {
	return obj.Pkg() != nil && obj.Pkg().Path() == jennyPath
}
