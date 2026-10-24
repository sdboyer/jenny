# jenny

jenny is a code generation framework for Go. It's for generators that take one set of definitions (schemas, API specs, config) and produce many kinds of output: types in several languages, registries, clients, docs.

Generators like that tend to become spaghetti. Without a rule for where new output belongs, one generator grows to emit several kinds of file. It sprouts knobs for each new variant and fills up with conditionals that check them. Outputs start reaching into each other's code, and the plumbing every generator needs ends up copied everywhere, slightly differently each time. That plumbing includes looping over inputs, saying which input failed, placing files, adding headers and catching path collisions.

jenny is built on one constraint: **a jenny maps an input type to a file with a single functional purpose.** Go types for a resource are one jenny. A list of endpoints that references those types is another, a many-input jenny over the same inputs. A jenny that produces several files is ordinarily an aggregator. Generator funcs don't call each other. Each jenny takes on one responsibility, and jennies are composed side by side. Anything that operates on another jenny's output, such as adding a header, is itself composed in through the framework.

The constraint is enforced by the language, not by convention. A jenny's signature is inputs in and files out, and its func never sees what other jennies produce. Where a hand-rolled generator would add a knob and a conditional, a jenny-based one usually adds a jenny. That doesn't guarantee cleanly defined responsibilities, but it tends to produce them.

Generator funcs are plain Go that turn inputs into files, written however you like. Everything between them is composition. You combine jennies into one tree that reads like the output it produces, and the framework does the plumbing. Then you write the result to disk, or in CI, check that what's on disk is up to date.

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
func goType(r Resource) (*jenny.File, error) {
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

It handles one resource and knows nothing about the framework, apart from returning a `*jenny.File`. Naming it makes it a jenny:

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
func routes(rs ...Resource) (*jenny.File, error)

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

`gofmt` and `header` are `FileMapper`s, which are plain `func(jenny.File) (jenny.File, error)`. `header` builds its comment from `File.From`, so one mapper puts the right generator name on every file. `.Prefix` and `.Map` decorate a single jenny, applied left to right. `jenny.Prefix` and `jenny.Map` decorate a group of them. For decorators of your own, use `.With(func(jenny.Jenny[I]) jenny.Jenny[I])`.

### 5. Adapt converts inputs for a subtree

The TypeScript side has a module of API paths per version, plus an index per resource. Those jennies are written against a different input type:

```go
type Version struct {
    Resource Resource
    N        int
}

func tsPaths(v Version) (*jenny.File, error)     // playlist/v1.ts
func tsIndex(vs ...Version) (*jenny.File, error) // playlist/index.ts
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

## Origins

jenny is the successor to [grafana/codejen](https://github.com/grafana/codejen). See [NOTICE](NOTICE).
