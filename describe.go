package jenny

import (
	"path"
	"reflect"
	"runtime"
	"strings"
)

// op is the composition operation that made a Jenny.
type op uint8

const (
	opEmpty op = iota // the zero Jenny
	opNew
	opPipeline
	opPrefix
	opMap
	opAdapt
	opPrefixBy
	opWith
)

// node records how a Jenny was composed, so that [Jenny.Mermaid] can draw it
// without running it. A node is made once, when its Jenny is, and never
// changes.
type node struct {
	op      op
	in      reflect.Type // the Jenny's input type; P for Adapt
	out     reflect.Type // Adapt: A
	name    string       // New: the jenny's name
	manyIn  bool         // New, Adapt: the func takes ...I
	manyOut bool         // New, Adapt: the func returns Files, or []A
	dir     string       // Prefix

	// fn is the func the operation was given: the generator func for New,
	// the adapter for Adapt, the directory func for PrefixBy, the wrapper for
	// With and the FileMapper for Map.
	fn symbol

	// site is where Map was called. A FileMapper made by BytesMapper or
	// TreeMapper is a closure with no name of its own, so the call is what
	// identifies it.
	site symbol

	// children are the jennies a Pipeline, Adapt or PrefixBy runs, or the one
	// jenny a Prefix, Map or With decorates.
	children []*node
}

// symbol locates a func, or a call, in source.
type symbol struct {
	pkg  string // import path
	name string // declared name: F, or T.M for a method; empty for a func literal or a call
	file string // base name
	line int
}

// describe returns the node recording how j was composed.
func (j Jenny[I]) describe() *node {
	if j.desc == nil {
		return &node{op: opEmpty, in: reflect.TypeFor[I]()}
	}
	return j.desc
}

func describeAll[I Input](js []Jenny[I]) []*node {
	out := make([]*node, len(js))
	for i, j := range js {
		out[i] = j.describe()
	}
	return out
}

// decorates returns a node for op applied to j.
func decorates[I Input](o op, j Jenny[I]) *node {
	return &node{op: o, in: reflect.TypeFor[I](), children: []*node{j.describe()}}
}

// funcSymbol locates the func value fn in source.
func funcSymbol(fn any) symbol {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func || v.IsNil() {
		return symbol{}
	}
	f := runtime.FuncForPC(v.Pointer())
	if f == nil {
		return symbol{}
	}
	file, line := f.FileLine(f.Entry())
	pkg, name := splitSymbol(f.Name())
	return symbol{pkg: pkg, name: name, file: baseName(file), line: line}
}

// jennyPkg returns the import path of this package.
func jennyPkg() string { return reflect.TypeFor[node]().PkgPath() }

// callerSite locates the call into this package that led here: the innermost
// frame outside it. Frames are read as CallersFrames reports them, so inlined
// calls are accounted for.
func callerSite() symbol {
	self := jennyPkg()
	pcs := make([]uintptr, 32)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		f, more := frames.Next()
		if pkg, _ := splitSymbol(f.Function); pkg != self {
			return symbol{pkg: pkg, file: baseName(f.File), line: f.Line}
		}
		if !more {
			return symbol{}
		}
	}
}

// splitSymbol splits a runtime func name, such as
// example.com/a/b%2ev2.(*T[...]).M-fm, into the import path of its package
// (example.com/a/b.v2) and its declared name (T.M). The name is empty for a
// func literal.
func splitSymbol(sym string) (string, string) {
	sym = stripTypeArgs(sym)
	slash := strings.LastIndexByte(sym, '/')
	dot := strings.IndexByte(sym[slash+1:], '.')
	if dot < 0 {
		return "", ""
	}
	dot += slash + 1
	// Dots in the last element of an import path are escaped, so that the
	// first dot after it ends the path.
	pkg := strings.ReplaceAll(sym[:dot], "%2e", ".")
	parts := strings.Split(strings.TrimSuffix(sym[dot+1:], "-fm"), ".")
	for i, p := range parts {
		p = strings.TrimSuffix(strings.TrimPrefix(p, "(*"), ")")
		if isClosure(p) {
			return pkg, ""
		}
		parts[i] = p
	}
	return pkg, strings.Join(parts, ".")
}

// isClosure reports whether elem, an element of a runtime func name, marks a
// func literal: func1, 1 for one nested in it, or the empty element in
// glob..func1.
func isClosure(elem string) bool {
	digits := strings.TrimPrefix(elem, "func")
	if digits == "" {
		return true
	}
	return strings.Trim(digits, "0123456789") == ""
}

// stripTypeArgs removes the bracketed type arguments from a name.
func stripTypeArgs(s string) string {
	if !strings.ContainsRune(s, '[') {
		return s
	}
	var (
		b     strings.Builder
		depth int
	)
	for _, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func baseName(file string) string {
	return path.Base(strings.ReplaceAll(file, `\`, "/"))
}
