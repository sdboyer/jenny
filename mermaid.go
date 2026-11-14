package jenny

import (
	"fmt"
	"path"
	"reflect"
	"regexp"
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
// The input type, and the type each [Builder.Adapt] converts to, is a rounded
// node. Every jenny made with [Builder.New] is a box, and so is every Adapt's
// adapter func. An arrow runs to each from the inputs it's given, labeled
// with the shape of its func: 1→1 for func(I) (File, error), N→1 for
// func(...I) (File, error), and so on.
//
// A frame surrounds the jennies that an Adapt, a [Builder.PrefixBy] or a
// [Jenny.With] runs, and the jennies of a [Pipeline] that [Jenny.Prefix] or
// [Jenny.Map] decorates. Its title names what applies to them, in the order
// it applies. A decorator applied to a single jenny is named in its box.
//
// Each jenny's box gives the directory its files are placed in, which is
// known before anything runs, with {T} for the directory a PrefixBy over Ts
// picks. It also says how many files the jenny makes: 1 file or N files, per
// input of whichever type it's run once per. A many-input jenny inside a
// single-input Adapt makes a file per input of the Adapt, not one in all.
//
// Without [DiagramDocs], a FileMapper that isn't a named func is labeled
// with the file and line of its Map call.
func (j Jenny[I]) Mermaid(opts ...DiagramOption) (string, error) {
	var cfg diagramConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	d := &diagram{}
	if cfg.docs {
		d.src = newDocSource()
	}
	return d.render(j.describe())
}

type diagram struct {
	src    *docSource // nil without DiagramDocs
	body   strings.Builder
	edges  []string
	clicks []string
	ids    int
	depth  int
	err    error // the first doc lookup error
}

// scope is what a jenny in the diagram is run over.
type scope struct {
	from string       // the node its inputs come from
	per  reflect.Type // the innermost input type it's run once per; nil if it's run once over all inputs
	path string       // the directory its files are placed in
}

func (d *diagram) render(root *node) (string, error) {
	from := d.typeNode(root.in)
	d.item(root, scope{from: from}, nil)
	if d.err != nil {
		return "", d.err
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	b.WriteString(d.body.String())
	for _, s := range d.edges {
		b.WriteString("    " + s + "\n")
	}
	for _, s := range d.clicks {
		b.WriteString("    " + s + "\n")
	}
	return b.String(), nil
}

// item draws n, under chain, the decorators applied to it, outermost first.
func (d *diagram) item(n *node, sc scope, chain []*node) {
	chain = chain[:len(chain):len(chain)] // appends below must not share
	switch n.op {
	case opPrefix:
		sc.path = path.Join(sc.path, n.dir)
		d.item(n.children[0], sc, append(chain, n))
	case opMap:
		d.item(n.children[0], sc, append(chain, n))
	case opPipeline:
		items := flatten(n.children)
		switch {
		case len(items) == 0:
			d.empty(sc, chain)
		case len(items) == 1:
			d.item(items[0], sc, chain)
		case len(chain) == 0:
			d.items(items, sc)
		default:
			d.subgraph(d.title("", chain), func() { d.items(items, sc) })
		}
	case opAdapt:
		d.subgraph(d.title("**Adapt**", chain), func() {
			adapter := d.id()
			d.line(adapter + `[["` + "`" + escLabel(funcLabel(n.fn)) + "`" + `"]]`)
			d.edge(sc.from, adapter, arity(n))
			d.funcClick(adapter, n.fn)
			inner := sc
			inner.from = d.typeNode(n.out)
			d.edge(adapter, inner.from, "")
			if !n.manyIn {
				inner.per = n.in
			}
			d.items(n.children, inner)
		})
	case opPrefixBy:
		sc.path = path.Join(sc.path, "{"+typeName(n.in)+"}")
		sc.per = n.in
		d.subgraph(d.title("**PrefixBy** "+escLabel(funcLabel(n.fn)), chain), func() {
			d.items(n.children, sc)
		})
	case opWith:
		d.subgraph(d.title("**With** "+escLabel(funcLabel(n.fn)), chain), func() {
			d.item(n.children[0], sc, nil)
		})
	case opNew:
		d.leaf(n, sc, chain)
	case opEmpty:
		d.empty(sc, chain)
	}
}

// items draws the jennies ns, flattening the Pipelines among them, or an
// empty jenny if there are none.
func (d *diagram) items(ns []*node, sc scope) {
	ns = flatten(ns)
	if len(ns) == 0 {
		d.empty(sc, nil)
	}
	for _, n := range ns {
		d.item(n, sc, nil)
	}
}

// flatten returns ns with every undecorated Pipeline among them replaced by
// the jennies it runs, which is the same thing.
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

func (d *diagram) leaf(n *node, sc scope, chain []*node) {
	id := d.id()
	head := "**" + escLabel(n.name) + "**"
	if fl := funcLabel(n.fn); fl != "" {
		head += " · " + escLabel(fl)
	}
	lines := []string{head}
	if len(chain) != 0 {
		lines = append(lines, d.title("", chain))
	}
	lines = append(lines, escLabel(placed(sc.path)), escLabel(fanOut(n, sc.per)))
	d.line(id + `["` + "`" + strings.Join(lines, "\n") + "`" + `"]`)
	d.edge(sc.from, id, arity(n))
	d.funcClick(id, n.fn)
}

func (d *diagram) empty(sc scope, chain []*node) {
	id := d.id()
	lines := []string{"(empty)"}
	if len(chain) != 0 {
		lines = append(lines, d.title("", chain))
	}
	d.line(id + `["` + "`" + strings.Join(lines, "\n") + "`" + `"]`)
	d.edge(sc.from, id, "")
}

func (d *diagram) typeNode(t reflect.Type) string {
	id := d.id()
	d.line(id + `(["` + "`" + escLabel(typeName(t)) + "`" + `"])`)
	d.typeClick(id, t)
	return id
}

func (d *diagram) subgraph(title string, body func()) {
	d.line("subgraph " + d.id() + `["` + "`" + title + "`" + `"]`)
	d.depth++
	body()
	d.depth--
	d.line("end")
}

// title joins head and chain, in the order the decorators in chain apply,
// innermost first.
func (d *diagram) title(head string, chain []*node) string {
	var parts []string
	if head != "" {
		parts = append(parts, head)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		switch n := chain[i]; n.op {
		case opPrefix:
			dir := n.dir
			if dir == "" {
				dir = `""`
			}
			parts = append(parts, "**Prefix** "+escLabel(dir))
		case opMap:
			parts = append(parts, "**Map** "+escLabel(d.mapperLabel(n)))
		}
	}
	return strings.Join(parts, " → ")
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
	if d.src == nil || d.err != nil {
		return
	}
	if t.Kind() == reflect.Pointer && t.Name() == "" {
		t = t.Elem()
	}
	if t.Name() == "" || t.PkgPath() == "" {
		return
	}
	d.click(id, t.PkgPath(), stripTypeArgs(t.Name()))
}

func (d *diagram) funcClick(id string, fn symbol) {
	if d.src == nil || d.err != nil || fn.name == "" {
		return
	}
	d.click(id, fn.pkg, fn.name)
}

func (d *diagram) click(id, pkg, name string) {
	doc, link, err := d.src.doc(pkg, name)
	if err != nil {
		d.err = err
		return
	}
	if doc == "" {
		return
	}
	// Import paths and Go identifiers hold nothing Mermaid needs escaped.
	d.clicks = append(d.clicks, fmt.Sprintf(`click %s href "https://pkg.go.dev/%s#%s" "%s"`, id, link, name, escTooltip(doc)))
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

func (d *diagram) edge(from, to, label string) {
	if label == "" {
		d.edges = append(d.edges, from+" --> "+to)
		return
	}
	d.edges = append(d.edges, from+` -->|"`+label+`"| `+to)
}

// arity is the shape of n's func: how many inputs it takes and how many
// outputs it returns.
func arity(n *node) string {
	in, out := "1", "1"
	if n.manyIn {
		in = "N"
	}
	if n.manyOut {
		out = "N"
	}
	return in + "→" + out
}

// fanOut says how many files a New jenny makes, given the innermost input
// type it's run once per.
func fanOut(n *node, per reflect.Type) string {
	qty := "1 file"
	if n.manyOut {
		qty = "N files"
	}
	switch {
	case !n.manyIn:
		return qty + " per " + typeName(n.in)
	case per != nil:
		return qty + " per " + typeName(per)
	default:
		return qty
	}
}

// placed is the directory a jenny's files are placed in, as shown on its box.
func placed(dir string) string {
	if dir == "" {
		return "./…"
	}
	return dir + "/…"
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
