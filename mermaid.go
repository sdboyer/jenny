package jenny

import (
	"fmt"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// DiagramOption configures [Jenny.Mermaid].
type DiagramOption func(*diagramConfig)

type diagramConfig struct {
	docs bool
}

// DiagramDocs makes [Jenny.Mermaid] read the source of the packages a
// pipeline's types and funcs are declared in. Their doc comments become
// tooltips, and each [Jenny.Map] is labeled with the FileMapper expression it
// was called with, such as header.
//
// Source is found the way the go command finds it, from the current
// directory, so the go command and the source must both be available. A
// package whose source can't be found or parsed is an error. A type or func
// without a doc comment simply has no tooltip. Types are looked up by name,
// so a type declared inside a func gets the doc of a package-level type with
// the same name, if there is one.
func DiagramDocs() DiagramOption {
	return func(c *diagramConfig) { c.docs = true }
}

// Mermaid returns a Mermaid flowchart of how the jenny is composed, drawn
// from its declaration alone: nothing is generated.
//
// The flowchart reads left to right, from the input type to the directories
// files are placed under. Input types are rounded, and each jenny made with
// [Builder.New] is a box. An arrow runs from a type to each jenny and each
// [Builder.Adapt] that's given it: a thin one to a func that takes one input,
// called once per input, and a thick one to a func that takes ...I, called
// once with all of them. An Adapt is drawn as the type it converts to, over
// the name of its adapter func.
//
// Each jenny has a dotted line to the directory its files are placed under,
// as far as that's known before anything runs: [Jenny.Prefix] is drawn only
// there, {T} is the directory a [Builder.PrefixBy] over Ts picks, and a
// FileMapper that moves files isn't accounted for. On the line is how many
// files the jenny makes, in terms of n, the number of inputs given to the
// jenny Mermaid is called on. A jenny that returns [Files], and an
// adapter that returns a slice, is marked with a factor of its own, ×k, or
// ×k₁, ×k₂ and so on, for how many it returns each time it's called. A count
// is the most a jenny makes: a nil File or an empty Files makes fewer.
//
// A frame titled with a FileMapper or a wrapper surrounds the jennies a
// [Jenny.Map] or [Jenny.With] applies to. A dashed frame surrounds the jennies
// that are run once per input: those in an Adapt whose adapter takes one
// input, and those in a PrefixBy. Within one, a jenny that takes ...I is given
// only what's made from one input at a time.
//
// The flowchart's front matter selects the ELK layout, which GitHub provides.
// Without DiagramDocs, a FileMapper that isn't a named func is labeled with
// the file and line of its Map call.
func (j Jenny[I]) Mermaid(opts ...DiagramOption) (string, error) {
	var cfg diagramConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	root := j.describe()
	d := &diagram{dirs: make(map[string]string), factors: fanOuts(root)}
	if cfg.docs {
		d.src = newDocSource()
	}
	return d.render(root)
}

type diagram struct {
	src     *docSource // nil without DiagramDocs
	body    strings.Builder
	dirs    map[string]string // directory node IDs, by directory
	outDefs []string          // count and directory nodes, in the order they're first used
	edges   []string
	clicks  []string
	each    []string // the frames around jennies run once per input
	factors map[*node]string
	ids     int
	depth   int
	err     error // the first doc lookup error
}

// scope is what a jenny in the diagram is run over.
type scope struct {
	from  string   // the node its inputs come from
	card  []string // how many inputs there are in all, as a product of factors
	calls []string // how many times a func that takes ...I is called, as a product
	path  string   // the directory its files are placed in
}

func (d *diagram) render(root *node) (string, error) {
	from := d.id()
	d.line(from + `(["` + "`**" + escLabel(typeName(root.in)) + "** ×n`" + `"])`)
	d.typeClick(from, root.in)
	// Everything between the input and the directories is framed, so that
	// the directories line up to the right of it.
	all := d.id()
	d.subgraph(all, " ", func() { d.item(root, scope{from: from, card: []string{"n"}}) })
	if d.err != nil {
		return "", d.err
	}
	var b strings.Builder
	b.WriteString("---\nconfig:\n  layout: elk\n---\nflowchart LR\n")
	b.WriteString(d.body.String())
	for _, group := range [][]string{d.outDefs, d.edges, d.clicks} {
		for _, s := range group {
			b.WriteString("    " + s + "\n")
		}
	}
	b.WriteString("    classDef hidden fill:none,stroke:none\n")
	b.WriteString("    class " + all + " hidden\n")
	if len(d.each) != 0 {
		b.WriteString("    classDef each fill:none,stroke-dasharray:4 3\n")
		b.WriteString("    class " + strings.Join(d.each, ",") + " each\n")
	}
	return b.String(), nil
}

func (d *diagram) item(n *node, sc scope) {
	switch n.op {
	case opPrefix:
		sc.path = path.Join(sc.path, n.dir)
		d.item(n.children[0], sc)
	case opMap:
		d.subgraph(d.id(), "**Map** "+escLabel(d.mapperLabel(n)), func() { d.item(n.children[0], sc) })
	case opWith:
		d.subgraph(d.id(), "**With** "+escLabel(funcLabel(n.fn)), func() { d.item(n.children[0], sc) })
	case opPipeline:
		d.items(n.children, sc)
	case opAdapt:
		if n.manyIn {
			d.adapt(n, sc)
			return
		}
		d.eachFrame(" ", func() { d.adapt(n, sc) })
	case opPrefixBy:
		sc.path = path.Join(sc.path, "{"+typeName(n.in)+"}")
		sc.calls = sc.card
		d.eachFrame("**PrefixBy** "+escLabel(funcLabel(n.fn)), func() { d.items(n.children, sc) })
	case opNew:
		d.leaf(n, sc)
	case opEmpty:
		d.empty(sc)
	}
}

// items draws the jennies ns, flattening the Pipelines among them, or an
// empty jenny if there are none.
func (d *diagram) items(ns []*node, sc scope) {
	ns = flatten(ns)
	if len(ns) == 0 {
		d.empty(sc)
	}
	for _, n := range ns {
		d.item(n, sc)
	}
}

// flatten returns ns with every Pipeline among them replaced by the jennies
// it runs, which is the same thing.
func flatten(ns []*node) []*node {
	var out []*node
	for _, n := range ns {
		if n.op == opPipeline {
			out = append(out, flatten(n.children)...)
		} else {
			out = append(out, n)
		}
	}
	return out
}

// adapt draws an Adapt as the type it converts to, and the jennies it runs
// over that type.
func (d *diagram) adapt(n *node, sc scope) {
	id := d.id()
	label := "**" + escLabel(typeName(n.out)) + "**\n*" + escLabel(funcLabel(n.fn)) + "*" + d.factor(n)
	d.line(id + `(["` + "`" + label + "`" + `"])`)
	d.feed(sc.from, id, n.manyIn)
	d.adaptClick(id, n)
	inner := scope{from: id, path: sc.path}
	if n.manyIn {
		inner.calls = sc.calls
	} else {
		inner.calls = sc.card
	}
	inner.card = times(inner.calls, d.factors[n])
	d.items(n.children, inner)
}

func (d *diagram) leaf(n *node, sc scope) {
	id := d.id()
	d.line(id + `["` + "`**" + escLabel(n.name) + "**" + d.factor(n) + "`" + `"]`)
	d.feed(sc.from, id, n.manyIn)
	count := sc.card
	if n.manyIn {
		count = sc.calls
	}
	d.output(id, times(count, d.factors[n]), sc.path)
	d.funcClick(id, n.fn)
}

func (d *diagram) empty(sc scope) {
	id := d.id()
	d.line(id + `["` + "`(empty)`" + `"]`)
	d.feed(sc.from, id, false)
}

// factor is the mark of the factor n multiplies what it's given by, if it
// can return many.
func (d *diagram) factor(n *node) string {
	if f := d.factors[n]; f != "" {
		return " ×" + f
	}
	return ""
}

func (d *diagram) subgraph(id, title string, body func()) {
	d.line("subgraph " + id + `["` + "`" + title + "`" + `"]`)
	d.depth++
	body()
	d.depth--
	d.line("end")
}

// eachFrame draws a dashed frame, around jennies run once per input.
func (d *diagram) eachFrame(title string, body func()) {
	id := d.id()
	d.each = append(d.each, id)
	d.subgraph(id, title, body)
}

// feed draws the arrow from a type to a func it's given to: thick for a func
// that takes ...I.
func (d *diagram) feed(from, to string, many bool) {
	arrow := " --> "
	if many {
		arrow = " ==> "
	}
	d.edges = append(d.edges, from+arrow+to)
}

// output draws the dotted line from a jenny to the directory its files are
// placed in, through how many files it makes. The count is a node of its own,
// without a border, rather than the line's label, which Mermaid draws on a
// background of its own without padding.
func (d *diagram) output(from string, count []string, dir string) {
	n := d.id()
	d.outDefs = append(d.outDefs, n+`["`+"`"+product(count)+"`"+`"]@{ shape: text }`)
	id, ok := d.dirs[dir]
	if !ok {
		id = d.id()
		d.dirs[dir] = id
		d.outDefs = append(d.outDefs, id+`["`+"`"+escLabel(dirLabel(dir))+"`"+`"]@{ shape: folder }`)
	}
	d.edges = append(d.edges, from+" -.- "+n+" -.- "+id)
}

// mapperLabel names the FileMapper given to a Map: by its own name if it's a
// named func, and otherwise by the expression Map was called with, given
// DiagramDocs, or the call's position, without.
func (d *diagram) mapperLabel(n *node) string {
	if n.fn.name != "" {
		return n.fn.name
	}
	if d.src == nil || d.err != nil {
		return fmt.Sprintf("@%s:%d", n.site.file, n.site.line)
	}
	label, err := d.src.mapArg(n.site)
	if err != nil {
		d.err = err
	}
	return label
}

func (d *diagram) typeClick(id string, t reflect.Type) {
	if pkg, name, ok := typeSymbol(t); ok {
		d.click(id, []symbol{{pkg: pkg, name: name}})
	}
}

func (d *diagram) funcClick(id string, fn symbol) {
	d.click(id, []symbol{fn})
}

// adaptClick gives an Adapt's node the docs of both the type it converts to
// and its adapter, linked to the type's.
func (d *diagram) adaptClick(id string, n *node) {
	var syms []symbol
	if pkg, name, ok := typeSymbol(n.out); ok {
		syms = append(syms, symbol{pkg: pkg, name: name})
	}
	d.click(id, append(syms, n.fn))
}

// typeSymbol returns the package and name of the type t, or of the type it
// points to, if it's named and declared in a package.
func typeSymbol(t reflect.Type) (string, string, bool) {
	if t.Kind() == reflect.Pointer && t.Name() == "" {
		t = t.Elem()
	}
	if t.Name() == "" || t.PkgPath() == "" {
		return "", "", false
	}
	return t.PkgPath(), stripTypeArgs(t.Name()), true
}

// click gives node id a tooltip of the docs of syms, the ones that have a
// doc comment, linked to the first of those.
func (d *diagram) click(id string, syms []symbol) {
	if d.src == nil || d.err != nil {
		return
	}
	var docs []string
	var link string
	for _, s := range syms {
		if s.name == "" {
			continue
		}
		doc, pkg, err := d.src.doc(s.pkg, s.name)
		if err != nil {
			d.err = err
			return
		}
		if doc == "" {
			continue
		}
		if link == "" {
			// Import paths and Go identifiers hold nothing Mermaid needs
			// escaped.
			link = "https://pkg.go.dev/" + pkg + "#" + s.name
		}
		docs = append(docs, doc)
	}
	if len(docs) != 0 {
		d.clicks = append(d.clicks, fmt.Sprintf(`click %s href "%s" "%s"`, id, link, escTooltip(strings.Join(docs, " "))))
	}
}

func (d *diagram) id() string {
	id := fmt.Sprintf("n%d", d.ids)
	d.ids++
	return id
}

// line writes a line of the diagram's body. Only the first line of s is
// indented: further lines continue a label, and must not be.
func (d *diagram) line(s string) {
	d.body.WriteString(strings.Repeat("    ", d.depth+1) + s + "\n")
}

// fanOuts names the factor that each jenny that returns Files, and each
// adapter that returns a slice, multiplies what it's given by: k if there's
// one, and otherwise k₁, k₂ and so on, in the order they're drawn.
func fanOuts(root *node) map[*node]string {
	var many []*node
	seen := make(map[*node]bool)
	var walk func(*node)
	walk = func(n *node) {
		if n.manyOut && !seen[n] {
			seen[n] = true
			many = append(many, n)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	names := make(map[*node]string, len(many))
	for i, n := range many {
		if len(many) == 1 {
			names[n] = "k"
			continue
		}
		names[n] = "k" + strings.Map(func(r rune) rune { return r - '0' + '₀' }, strconv.Itoa(i+1))
	}
	return names
}

// times returns the product fs multiplied by f, if there is an f.
func times(fs []string, f string) []string {
	if f == "" {
		return fs
	}
	return append(fs[:len(fs):len(fs)], f)
}

func product(fs []string) string {
	if len(fs) == 0 {
		return "1"
	}
	return strings.Join(fs, "·")
}

// dirLabel is a directory, as its node shows it.
func dirLabel(dir string) string {
	if dir == "" {
		return "./"
	}
	return dir + "/"
}

func funcLabel(fn symbol) string {
	switch {
	case fn.name != "":
		return fn.name
	case fn.file != "":
		return fmt.Sprintf("func@%s:%d", fn.file, fn.line)
	default:
		return ""
	}
}

// pathElems matches the import path elements before a package name in a
// type's name, as in Tree[example.com/a/b.T].
var pathElems = regexp.MustCompile(`[\w.%-]+/`)

// typeName is t as it's written in the package that declares it.
func typeName(t reflect.Type) string {
	switch {
	case t == reflect.TypeFor[any]():
		return "any"
	case t.Name() != "":
		return pathElems.ReplaceAllString(t.Name(), "")
	default:
		return t.String()
	}
}

// escTooltip makes s safe as a Mermaid tooltip. Mermaid sets the tooltip as
// HTML from an attribute, which decodes entities once, so the characters HTML
// reads as markup are written as entities twice over.
func escTooltip(s string) string {
	return strings.NewReplacer(
		"#", "#35;",
		`"`, "#quot;",
		"&", "#amp;amp;",
		"<", "#amp;lt;",
		">", "#amp;gt;",
	).Replace(s)
}

// escLabel makes s safe within a Mermaid markdown string: the characters
// Mermaid or HTML would read as markup are written as entities, a backtick,
// which would end the string, as a quote, and the characters markdown reads
// as emphasis are escaped.
func escLabel(s string) string {
	return strings.NewReplacer(
		"#", "#35;",
		`"`, "#quot;",
		"&", "#amp;",
		"<", "#lt;",
		">", "#gt;",
		"`", "'",
		`\`, `\\`,
		"*", `\*`,
		"_", `\_`,
		"~", `\~`,
	).Replace(s)
}
