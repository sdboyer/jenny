# jenny

jenny is a code generation framework for Go. It's for generators that take one set of definitions (schemas, API specs, config) and produce many kinds of output: types in several languages, registries, clients, docs.

Generators like that tend to become spaghetti. Without a rule for where new output belongs, one generator grows to emit several kinds of file. It sprouts knobs for each new variant and fills up with conditionals that check them. Outputs start reaching into each other's code, and the plumbing every generator needs ends up copied everywhere, slightly differently each time. That plumbing includes looping over inputs, saying which input failed, placing files, adding headers and catching path collisions.

jenny is built on one constraint: **a jenny maps an input type to a file with a single functional purpose.** Go types for a resource are one jenny. A list of endpoints that references those types is another, a many-input jenny over the same inputs. A jenny that produces several files is ordinarily an aggregator. Generator funcs don't call each other. Each jenny takes on one responsibility, and jennies are composed side by side. Anything that operates on another jenny's output, such as adding a header, is itself composed in through the framework.

The constraint is enforced by the language, not by convention. A jenny's signature is inputs in and files out, and its func never sees what other jennies produce. Where a hand-rolled generator would add a knob and a conditional, a jenny-based one usually adds a jenny. That doesn't guarantee cleanly defined responsibilities, but it tends to produce them.

Generator funcs are plain Go that turn inputs into files, written however you like. Everything between them is composition. You combine jennies into one tree that reads like the output it produces, and the framework does the plumbing. Then you write the result to disk, or in CI, check that what's on disk is up to date. You can also draw the tree, to see what it produces where without running it.

The walkthrough below builds a generator for a small API one piece at a time. Every snippet is from [readme_test.go](readme_test.go), which checks the outputs shown here.

Requires Go 1.27 (generic methods).

## Walkthrough

The input is a list of API resources:

```go
type Resource struct {
    Name     string
    Fields   []Field
    Versions []int // API versions that serve this resource
}

var resources = []Resource{
    {Name: "playlist", Fields: []Field{{"name", "string"}, {"interval", "int"}}, Versions: []int{1, 2}},
    {Name: "team", Fields: []Field{{"name", "string"}, {"email", "string"}}, Versions: []int{1}},
}
```

### 1. A generator is a plain func

Here's one that makes a Go struct for a resource:

```go
func goType(r Resource) (jenny.File, error) {
    var b strings.Builder
    fmt.Fprintf(&b, "package api\n\ntype %s struct {\n", title(r.Name))
    for _, f := range r.Fields {
        t, ok := goFieldTypes[f.Type]
        if !ok {
            return nil, fmt.Errorf("unsupported field type %q", f.Type)
        }
        fmt.Fprintf(&b, "\t%s %s\n", title(f.Name), t)
    }
    b.WriteString("}\n")
    return jenny.NewFile(r.Name+".go", []byte(b.String())), nil
}
```

It handles one resource and knows nothing about the framework, apart from returning a `jenny.File`. `jenny.NewFile` makes a file from bytes. Naming it makes it a jenny:

```go
api := jenny.For[Resource](jenny.InputName(func(r Resource) string { return r.Name }))

goTypes := api.New("GoTypes", goType)
goTypes.Generate(resources...)
```
```
playlist.go (GoTypes)
team.go (GoTypes)
```

`For` is the only place you name the input type; everything after that is inferred. Because `goType` takes a single `Resource`, the jenny calls it once per resource. Every file is stamped with the name of the jenny that made it (`File.From`, shown in parentheses). Errors get labeled for you too, with the jenny's name and, through `InputName`, the input that failed:

```
GoTypes: unsupported field type "decimal" for input "invoice"
```

### 2. Some outputs need every input at once

A route table covers all resources, so its func takes all of them:

```go
func routes(rs ...Resource) (jenny.File, error)

api.New("Routes", routes)
```
```go
package api

var Routes = map[string]any{
	"/playlist": Playlist{},
	"/team": Team{},
}
```

`routes` references the types `goType` generates, but it doesn't call `goType` or depend on its output. It's a separate jenny with its own purpose. The two agree on type names because both use the same plain helper, `title`. Sharing ordinary helpers like that is how jennies share logic. They also agree implicitly on a Go package: both write `package api`, and `routes.go` only compiles next to the types. Step 4 puts them both in `pkg/api`.

`New` accepts four func shapes: one input or many, one file or many. The shape decides how the func is called. The result is the same `Jenny[Resource]` either way, so nothing below this point cares which shape a jenny has.

### 3. Pipelines combine jennies

```go
jenny.Pipeline(
    api.New("GoTypes", goType),
    api.New("Routes", routes),
)
```
```
playlist.go (GoTypes)
routes.go (Routes)
team.go (GoTypes)
```

Every jenny in a pipeline gets the same inputs, and their outputs share one path namespace. Two jennies that claim the same path (case-insensitively, so it also works on macOS and Windows) cause an error, not a silent overwrite:

```
multiple files at path playlist.go from jennies: 'GoTypes', 'Bundle'
```

If several jennies fail, the pipeline reports every error, not just the first one.

### 4. Decorators place and post-process output

Generator funcs write paths relative to wherever their output ends up, and they don't format or annotate it. Those concerns are added around them:

```go
jenny.Map(header,
    jenny.Prefix("pkg/api",
        api.New("GoTypes", goType),
        api.New("Routes", routes),
    ).Map(gofmt),
)
```
```
pkg/api/playlist.go (GoTypes)
pkg/api/routes.go (Routes)
pkg/api/team.go (GoTypes)
```
```go
// Code generated by GoTypes. DO NOT EDIT.

package api

type Playlist struct {
	Name     string
	Interval int64
}
```

`gofmt` and `header` are `FileMapper`s. Both are made by `jenny.BytesMapper` from a plain `func(jenny.File, []byte) ([]byte, error)`, which gets each file's content and returns the new content. `header` builds its comment from `File.From`, so one mapper puts the right generator name on every file. (A file can also be carried as a syntax tree, a `jenny.Tree`, which `jenny.TreeMapper`s work on directly. The tree can be in any language: a `go/ast` tree, a CUE one, or a type of your own, with a func that renders it. Tree work goes before byte work, because a `BytesMapper` renders the tree, and a `TreeMapper` after it reports an error. See `ExampleTreeMapper`, and `ExampleTreeMapper_typeScript` for a tree that isn't Go.) `.Prefix` and `.Map` decorate a single jenny, applied left to right. `jenny.Prefix` and `jenny.Map` decorate a group of them. For decorators of your own, use `.With(func(jenny.Jenny[I]) jenny.Jenny[I])`.

### 5. Adapt converts inputs for a subtree

The TypeScript side has a module of API paths per version, plus an index per resource. Those jennies are written against a different input type:

```go
type Version struct {
    Resource Resource
    N        int
}

func tsPaths(v Version) (jenny.File, error)     // playlist/v1.ts
func tsIndex(vs ...Version) (jenny.File, error) // playlist/index.ts
```

`Adapt` takes a func that converts inputs, then runs a pipeline of jennies over what it returns:

```go
func versions(r Resource) ([]Version, error)

ver := jenny.For[Version](jenny.InputName(func(v Version) string { return fmt.Sprintf("%s/v%d", v.Resource.Name, v.N) }))

api.Adapt(versions,
    ver.New("TSPaths", tsPaths),
    ver.New("TSIndex", tsIndex),
).Prefix("web/src/api")
```
```
web/src/api/playlist/index.ts (TSIndex)
web/src/api/playlist/v1.ts (TSPaths)
web/src/api/playlist/v2.ts (TSPaths)
web/src/api/team/index.ts (TSIndex)
web/src/api/team/v1.ts (TSPaths)
```

The adapter's shape decides how the subtree runs, the same way a generator func's shape does. `versions` takes one `Resource`, so the subtree runs once per resource. That's why `tsIndex`, which takes many inputs, gets one resource's versions at a time and writes one index per resource. If the adapter took `...Resource`, the subtree would run once over every version of every resource.

Jennies in a subtree don't know they're in one. `tsPaths` behaves the same when it's run directly over a list of `Version`s, so it can be tested and reused on its own. Adapter errors are labeled like generator errors. Adapters can also filter: nil results are dropped, and a subtree with no inputs doesn't run.

`tsPaths` and `tsIndex` each write the resource's name into their paths. When a directory depends on the input, `PrefixBy` can add it instead, so they write only `v1.ts` and `index.ts`:

```go
func tsVersionFile(v Version) (jenny.File, error)   // v1.ts
func tsIndexFile(vs ...Version) (jenny.File, error) // index.ts

api.PrefixBy(func(r Resource) string { return r.Name },
    api.Adapt(versions,
        ver.New("TSPaths", tsVersionFile),
        ver.New("TSIndex", tsIndexFile),
    ),
).Prefix("web/src/api")
```

The output is the same as above. `PrefixBy` is `Prefix` with a directory that depends on the input, and it follows the same path rules: an empty directory means no prefix, and two inputs that place a file at the same path are an error. It sees the inputs it's given, so here, outside the `Adapt`, it gets resources; inside it, it would get versions. Its func takes one input, so like a single-input adapter, it runs its jennies once per input, and each file comes from exactly one input. A many-input jenny inside it writes one file per input, so one that should see every input goes beside `PrefixBy`, not inside it.

### 6. Put it together

```go
gen := jenny.Map(header,
    jenny.Prefix("pkg/api",
        api.New("GoTypes", goType),
        api.New("Routes", routes),
    ).Map(gofmt),
    api.Adapt(versions,
        ver.New("TSPaths", tsPaths),
        ver.New("TSIndex", tsIndex),
    ).Prefix("web/src/api"),
)
```
```
pkg/api/playlist.go (GoTypes)
pkg/api/routes.go (Routes)
pkg/api/team.go (GoTypes)
web/src/api/playlist/index.ts (TSIndex)
web/src/api/playlist/v1.ts (TSPaths)
web/src/api/playlist/v2.ts (TSPaths)
web/src/api/team/index.ts (TSIndex)
web/src/api/team/v1.ts (TSPaths)
```

The tree reads like the output. Each generator func does one thing, and every cross-cutting concern sits on exactly one line. To add an output, add a line. To move an output, change a `Prefix`.

To run it, generate into an in-memory `FS` and then write it, or in CI, verify it:

```go
fs, err := gen.GenerateFS(resources...)
if err != nil {
    return err
}
if os.Getenv("CODEGEN_VERIFY") != "" {
    return fs.Verify(ctx, root) // reports files that are missing or differ
}
return fs.Write(ctx, root)
```

### 7. Draw it

`Mermaid` draws a jenny as a [Mermaid](https://mermaid.js.org) flowchart, from its declaration alone. Nothing is generated:

```go
diagram, err := gen.Mermaid(jenny.DiagramDocs())
```
```mermaid
---
config:
  layout: elk
---
flowchart LR
    n0(["`**Resource** ×n`"])
    subgraph n1["` `"]
        subgraph n2["`**Map** header`"]
            subgraph n3["`**Map** gofmt`"]
                n4["`**GoTypes**`"]
                n7["`**Routes**`"]
            end
            subgraph n9["` `"]
                n10(["`**Version**
*versions* ×k`"])
                n11["`**TSPaths**`"]
                n14["`**TSIndex**`"]
            end
        end
    end
    n5["`n`"]@{ shape: text }
    n6["`pkg/api/`"]@{ shape: folder }
    n8["`1`"]@{ shape: text }
    n12["`n·k`"]@{ shape: text }
    n13["`web/src/api/`"]@{ shape: folder }
    n15["`n`"]@{ shape: text }
    n0 --> n4
    n4 -.- n5 -.- n6
    n0 ==> n7
    n7 -.- n8 -.- n6
    n0 --> n10
    n10 --> n11
    n11 -.- n12 -.- n13
    n10 ==> n14
    n14 -.- n15 -.- n13
    click n0 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#Resource" "Resource is one resource in an API definition."
    click n10 href "https://pkg.go.dev/github.com/sdboyer/jenny_test#Version" "Version is one API version of a Resource."
    classDef hidden fill:none,stroke:none
    class n1 hidden
    classDef each fill:none,stroke-dasharray:4 3
    class n9 each
```

Read it left to right, from the input type to the directories files are placed under. An arrow runs from a type to each func that's given it: a thin one to a func called once per input, and a thick one to a func that takes `...I`, like `Routes`, called once with all of them. An `Adapt` is drawn as the type it converts to, over the name of its adapter.

On each dotted line is how many files a jenny makes from n resources. `versions` returns several versions for each resource, so it's marked `×k`, and `TSPaths`, run once per version, makes n·k files. A jenny that returns `Files` is marked the same way. A frame shows where a `Map` applies, and a dashed frame what's run once per input: `TSIndex` makes a file per resource, not one in all, because it's inside a single-input `Adapt`, as step 5 explains. A jenny that returns nothing for some input makes fewer.

`DiagramDocs` reads the source of the packages the types and funcs come from. Their doc comments become tooltips (hover over `Resource`), and each `Map` is labeled with the `FileMapper` it was given. Without it, a mapper that isn't a named func is labeled with the line `Map` was called on.

## Asking what a File holds

`jenny.File` is an interface, with two kinds: `jenny.Bytes` and `jenny.Tree[T]`. Pointers to them satisfy it too, so a `File` can hold a nil `*jenny.Bytes`, which isn't `== nil`. jenny turns pointers into values wherever your code hands it files, so a `FileMapper` is given only values, and a case naming `*jenny.Bytes` never matches.

So never compare a `jenny.File` with nil, type-switch on one or type-assert one. Ask with these instead, which treat a pointer exactly as the value it points to:

```go
jenny.IsNil(f)               // not f == nil
v, ok := jenny.TreeOf[T](f)  // not f.(jenny.Tree[T])
data, ok := jenny.BytesOf(f) // not f.(jenny.Bytes)
```

`f.Render()` returns any file's content as bytes, whatever its kind.

The `jennylint` analyzer finds code that breaks the rule:

```sh
go install github.com/sdboyer/jenny/jennylint/cmd/jennylint@latest
go vet -vettool=$(which jennylint) ./...
```

It reports:

- a type-switch case or type assertion on a `jenny.File` that names `*jenny.Bytes` or `*jenny.Tree[T]`. It never matches, so it's a bug.
- one that names `jenny.Bytes` or `jenny.Tree[T]`. It matches the files jenny hands out, but misses a pointer from anywhere else, so jennylint suggests `BytesOf` or `TreeOf`, which match both.
- a `jenny.File` compared with nil, or a `case nil` in a type switch on one. It misses a nil pointer, so jennylint suggests `IsNil`. `jennylint -fix ./...` rewrites the comparisons.

To run it in golangci-lint, build a custom golangci-lint with jennylint as a [module plugin](https://golangci-lint.run/docs/plugins/module-plugins/). In `.custom-gcl.yml`:

```yaml
version: v2.12.2 # your golangci-lint version
plugins:
  - module: github.com/sdboyer/jenny/jennylint
    import: github.com/sdboyer/jenny/jennylint/golangci
    version: latest
```

In `.golangci.yml`:

```yaml
linters:
  enable:
    - jennylint
  settings:
    custom:
      jennylint:
        type: module
        description: checks for nil comparisons, type switches and type assertions on a jenny.File
```

Then `golangci-lint custom` builds `./custom-gcl`, which you run in place of `golangci-lint`.

## Origins

jenny is the successor to [grafana/codejen](https://github.com/grafana/codejen). See [NOTICE](NOTICE).
