## Asking what a jenny.File holds

`jenny.File` is an interface, and `*jenny.Bytes` and `*jenny.Tree[T]` satisfy
it too. A File holding a nil pointer is not `== nil`, and jenny hands out only
values, so a case naming a pointer kind never matches. The rule, in jenny's own
code outside package jenny, in examples, in tests and in anything written for
users:

- Never compare a `jenny.File` with nil. Use `jenny.IsNil(f)`.
- Never type-switch on or type-assert a `jenny.File`. Use
  `jenny.TreeOf[T](f)` for a Tree's value and `jenny.BytesOf(f)` for a Bytes's
  data. `f.Render()` gets any File's bytes.
- To change a Tree's value, use `jenny.TreeMapper`; to change bytes, use
  `jenny.BytesMapper`; to change a path, use `jenny.Move`.

Package jenny itself is the exception: `normalize` is where pointers become
values, and code after it may rely on that. The `jennylint` analyzer
(`jennylint/`, a module of its own) enforces the rule. Before finishing a
change, run it the way CI does:

    (cd jennylint && go install ./cmd/jennylint)
    go vet -vettool="$(go env GOPATH)/bin/jennylint$(go env GOEXE)" ./...

jennylint is a separate module, so changes to it also need `go vet`,
`go test -race` and `go mod tidy -diff` run inside `jennylint/` (and
`go mod tidy -diff` in `jennylint/testdata`).

## Thresh seams

Before finalizing an approach that touches any of these, run `thresh check` and
read the taps. They are facts about your diff, not opinions, and they never block.

- **jenny-ctor** (`choke-point`): in *.go, only newJenny may construct Jenny. Constructing it elsewhere skips what the choke point enforces.
