# jenny

jenny is a modular code generation framework for Go. It builds generators out of small, single-purpose generator funcs ("jennies"), then writes their output to disk or, in CI, verifies that what's on disk is up to date.

```go
group := jenny.For[Group](jenny.InputName(func(g Group) string { return g.Name }))
typ := jenny.For[Type]()

gen := jenny.Map(header("gen.go"),
    group.New("GroupDocs", genGroupDocs).Prefix("docs"),
    group.Adapt(splitGroup,                // func(Group) ([]Type, error)
        typ.New("GoTypes", genGoTypes).Prefix("go"),
        typ.New("TSTypes", genTSTypes).Prefix("ts"),
        typ.New("TypeIndex", genIndex),    // func(...Type) (*jenny.File, error)
    ),
)

fs, err := gen.GenerateFS(groups...)
// ...
if os.Getenv("CODEGEN_VERIFY") != "" {
    err = fs.Verify(ctx, root)
} else {
    err = fs.Write(ctx, root)
}
```

- A generator func takes one or many inputs and returns one or many files. `New` accepts all four shapes and turns any of them into a `Jenny[I]`. Everything else works on `Jenny[I]` regardless of shape.
- `Adapt` converts inputs to another type and runs a nested pipeline over the results. The adapter func's signature (one or many in, one or many out) decides how often the nested pipeline runs. Nested jennies don't know they're nested.
- `Prefix`, `Map` and `With` decorate a single jenny, left to right. The `jenny.Prefix` and `jenny.Map` funcs decorate a group of them.
- The framework handles error wrapping, nil outputs, path-collision checks and `File.From` provenance. Generator funcs just generate.

Requires Go 1.27 (generic methods).

## Origins

jenny is the successor to [grafana/codejen](https://github.com/grafana/codejen). See [NOTICE](NOTICE).
