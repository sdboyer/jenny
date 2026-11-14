package jenny

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc/comment"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// docSource reads the source of the packages a diagram names, for
// [DiagramDocs].
type docSource struct {
	pkgs map[string]*docPkg // by the import path a symbol gives
}

func newDocSource() *docSource {
	return &docSource{pkgs: make(map[string]*docPkg)}
}

// docPkg is the parsed source of one package.
type docPkg struct {
	path  string // import path, with main resolved
	fset  *token.FileSet
	files map[string]*ast.File // by base name
	docs  map[string]string    // doc text of types and funcs, by name: T, F or T.M
}

// doc returns the doc comment of the type or func name in package pkg, as
// plain text, and the import path of the package.
func (s *docSource) doc(pkg, name string) (string, string, error) {
	p, err := s.pkg(pkg)
	if err != nil {
		return "", "", err
	}
	return p.docs[name], p.path, nil
}

func (s *docSource) pkg(importPath string) (*docPkg, error) {
	if p, ok := s.pkgs[importPath]; ok {
		return p, nil
	}
	p, err := loadDocPkg(importPath)
	if err != nil {
		return nil, fmt.Errorf("Mermaid: reading the source of package %s: %w", importPath, err)
	}
	s.pkgs[importPath] = p
	return p, nil
}

// mainPath is the import path of the main package, which symbols declared in
// it name as main.
func mainPath() (string, error) {
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Path == "" {
		return "", errors.New("the binary has no build info to give the import path of package main")
	}
	return bi.Path, nil
}

func loadDocPkg(importPath string) (*docPkg, error) {
	path := importPath
	if path == "main" {
		var err error
		if path, err = mainPath(); err != nil {
			return nil, err
		}
	}
	// The external test package of a package is in the same directory, and
	// isn't importable itself.
	base, xtest := strings.CutSuffix(path, "_test")
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	bp, err := build.Default.Import(base, cwd, build.FindOnly)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(bp.Dir)
	if err != nil {
		return nil, err
	}

	p := &docPkg{path: path, fset: token.NewFileSet(), files: make(map[string]*ast.File), docs: make(map[string]string)}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if ok, err := build.Default.MatchFile(bp.Dir, name); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		f, err := parser.ParseFile(p.fset, filepath.Join(bp.Dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(f.Name.Name, "_test") != xtest {
			continue
		}
		p.files[name] = f
		p.collect(f)
	}
	if len(p.files) == 0 {
		return nil, fmt.Errorf("no Go files in %s", bp.Dir)
	}
	return p, nil
}

// collect records the doc comments of f's package-level types and funcs.
func (p *docPkg) collect(f *ast.File) {
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.GenDecl:
			if decl.Tok != token.TYPE {
				continue
			}
			for _, spec := range decl.Specs {
				ts := spec.(*ast.TypeSpec)
				cg := ts.Doc
				if cg == nil && !decl.Lparen.IsValid() {
					cg = decl.Doc
				}
				p.add(ts.Name.Name, cg)
			}
		case *ast.FuncDecl:
			name := decl.Name.Name
			if decl.Recv != nil && len(decl.Recv.List) == 1 {
				name = recvName(decl.Recv.List[0].Type) + "." + name
			}
			p.add(name, decl.Doc)
		}
	}
}

func (p *docPkg) add(name string, cg *ast.CommentGroup) {
	if cg == nil {
		return
	}
	if _, ok := p.docs[name]; ok {
		return
	}
	var cp comment.Parser
	var pr comment.Printer
	if text := strings.Join(strings.Fields(string(pr.Text(cp.Parse(cg.Text())))), " "); text != "" {
		p.docs[name] = text
	}
}

// recvName is the name of the type a method's receiver has.
func recvName(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

// mapArg returns the source of the FileMapper that the Map call at site was
// given, such as header, jenny.TreeMapper(…) or func@gen.go:12.
func (s *docSource) mapArg(site symbol) (string, error) {
	p, err := s.pkg(site.pkg)
	if err != nil {
		return "", err
	}
	f := p.files[site.file]
	if f == nil {
		return "", fmt.Errorf("Mermaid: no file %s in the source of package %s, where Map was called", site.file, site.pkg)
	}
	var calls []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) != 0 && isMap(call.Fun) && p.fset.Position(call.Lparen).Line == site.line {
			calls = append(calls, call)
		}
		return true
	})
	switch len(calls) {
	case 0:
		return "", fmt.Errorf("Mermaid: no call to Map at %s:%d, where Map was called", site.file, site.line)
	case 1:
		return p.exprLabel(calls[0].Args[0]), nil
	default:
		return "", fmt.Errorf("Mermaid: more than one call to Map at %s:%d, so which FileMapper is which can't be told; put each on a line of its own", site.file, site.line)
	}
}

// isMap reports whether fun, the func of a call, could be Map or Jenny.Map.
func isMap(fun ast.Expr) bool {
	switch fun := fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel.Name == "Map"
	case *ast.Ident:
		return fun.Name == "Map"
	}
	return false
}

func (p *docPkg) exprLabel(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.FuncLit:
		pos := p.fset.Position(e.Pos())
		return fmt.Sprintf("func@%s:%d", filepath.Base(pos.Filename), pos.Line)
	case *ast.CallExpr:
		return p.exprString(e.Fun) + "(…)"
	default:
		return p.exprString(e)
	}
}

func (p *docPkg) exprString(e ast.Expr) string {
	var b bytes.Buffer
	// Printing to a bytes.Buffer fails only for a node that isn't valid
	// syntax, and e was parsed.
	_ = printer.Fprint(&b, p.fset, e)
	return strings.Join(strings.Fields(b.String()), " ")
}
