# File as an interface

Status: draft design, with a working prototype on `design/file-interface`.
Ownership claims and stale-file detection (#15), which were part of an
earlier version of this proposal, are now a separate PR stacked on this one.

`File` changes from a struct to a sealed interface, so that a file's content
can travel as something other than bytes (#36). For example, a Go file can
travel as a `go/ast` tree, a CUE file as a `cue/ast` tree, or a TypeScript file
as whatever TS AST a generator uses, so that mappers like formatting, header
insertion or import fixing work on the tree, and it's rendered to bytes only
when something needs bytes. Every method on the interface is there because of
the two content kinds below.

Nothing in jenny assumes the tree is Go. A `Tree[T]` is a value of any type
and a render func for it: jenny never parses, formats or inspects the value,
and formatting is whatever the render func does. `ExampleTreeMapper` uses a
`go/ast` tree; `ExampleTreeMapper_typeScript` puts it side by side with a
small TypeScript tree defined in the example itself, which stands in for a
real AST package without adding a dependency to jenny's module.

Questions marked **[Sam]** are open for Sam to decide. Each notes what the
prototype does in the meantime.

## The prototype's API

```go
type File interface {
    Path() string
    From() []NamedJenny
    FromString() string
    Render() ([]byte, error)
    Validate() error
    withPath(fn func(string) string) File // sealed; used by Prefix and Move
    stamped(nj NamedJenny) File           // sealed; used by New
}

type Bytes struct{ ... }       // NewFile(path, data, from...) Bytes; Data() []byte
type Tree[T any] struct{ ... } // NewTree(path, value, render, from...) Tree[T]; Value() T

func Move(f File, path string) File

func IsNil(f File) bool
func TreeOf[T any](f File) (T, bool)
func BytesOf(f File) ([]byte, bool)

type FileMapper func(File) (File, error)
func BytesMapper(fn func(f File, data []byte) ([]byte, error)) FileMapper
func TreeMapper[T any](fn func(f File, value T) (T, error)) FileMapper
```

The `GenFunc` shapes change from `*File` to `File`, e.g.
`func(I) (File, error)`.

## The method set

What each method is there for:

- **`Path`, `From` and `FromString`.** The framework does several things to a
  file without caring what's in it: placing it, stamping it, detecting
  collisions, and naming it in errors. These three methods are all those jobs
  need.
- **`Render`.** Bytes are what both kinds have in common. `FS`, `Verify`, and
  any mapper that edits bytes call it. A `Bytes` returns its data; a `Tree`
  calls its render func. Only one form of the content is ever stored, so the
  bytes and the tree can't disagree: the "one representation per value" rule
  from #36.
- **`Validate`.** The kinds don't validate the same way: a `Tree` also needs a
  render func. The path rules themselves are #37's portable `checkPath`.
- **`withPath` and `stamped` (unexported).** These let `Prefix`, `Move` and
  `New` rewrite a file's path or provenance and get back the *same kind*. A
  `Tree[T]` that goes through `Prefix` is still a `Tree[T]` afterwards, so a
  later `TreeMapper` still finds it.

## The nil-interface hazard

This hazard is why `File` wasn't an interface in the first place (#36).
Requirement: **authors can never hit the nil trap; the framework handles it,
once, at the boundary.** Specifically, a nil `*Bytes` or `*Tree[T]` handed to
jenny as a `File` behaves exactly like a nil `File`.

### Options

- **Make `*Bytes` and `*Tree[T]` not satisfy `File`.** This can't be done in
  Go. The method set of `*T` always includes every method of `T`, so if
  `Bytes` implements `File`, `*Bytes` does too, however the methods are
  arranged across embedded or unexported types. The only way out is to make
  the pointer the implementing type and stop the value from satisfying
  `File`, but that just moves the trap: a nil `*Bytes` becomes the expected
  return value, and it still isn't `== nil`.
- **Normalize at the boundary (prototype).** Every place where user code hands
  jenny a `File` calls one function, `normalize`:
  - each `GenFunc` shape, in `New`, for single returns and for each element
    of a `Files`;
  - every `FileMapper` result, in `Jenny.Map` and `FS.Map`;
  - `FS.Add`;
  - `Files.Validate`;
  - `Move`.

  `normalize` turns a nil pointer into a nil `File`, and a non-nil pointer
  into the value it points to, so that a `TreeMapper` downstream still finds
  its `Tree[T]` and never a `*Tree[T]`. Below the boundary, every file is a
  non-nil value of one of the kinds. Sealing makes this enforceable: the
  framework knows every kind implements `File` with value receivers, so
  dereferencing a pointer always yields a `File`, and users can't add a kind
  whose nil pointer means something.
- **Concrete types in, interfaces inside** (from the #36 discussion). Generator
  funcs would return concrete kinds and the framework would box them, but:
  - `GenFunc` is a union of func types over `I`. A `Tree[T]` shape brings a
    second type parameter that `GenFunc[I]` and `Builder[I].New` have nowhere
    to bind, so `New` would need a type argument for each representation,
    against the rule that only `For[...]` names a type.
  - A func that returns both Go trees and TS bytes needs a heterogeneous slice,
    which means an interface again, with nil elements still possible.
  - The four shapes would grow to four per kind.
- **An opaque handle.** `File` would be a small struct wrapping the sealed
  interface, with a zero `File{}` meaning "nothing", and the same accessors
  (`TreeOf`, `BytesOf`) instead of type assertions. This removes the trap by
  construction, with no reflection, where the prototype's helpers and
  `jennylint` remove it by rule and by check. The cost is that `File` is no
  longer an interface. Since mappers now use the accessors anyway, the
  remaining difference is whether `f == nil` and `f.(jenny.Bytes)` compile at
  all. **[Sam]**

### What a nil means where

| Where | nil (or a nil pointer) means |
| --- | --- |
| `func(...) (File, error)` return | nothing generated |
| element of a returned `Files` | an error naming the jenny (as a zero `File` in `Files` was on main) |
| `FileMapper` result | an error: a mapper can't drop a file |
| `FS.Add`, `Files.Validate` | an error |
| `Move(nil, p)` | nil |

`TestNilPointersAreNilFiles`, `TestNilPointersInFiles` and
`TestFileMapperNilPointers` run the review's probe against all four `GenFunc`
shapes, `Files` elements, `FileMapper` results, `FS.Add`, `FS.Map`,
`Files.Validate` and `Move`. They cover both `*Bytes` and `*Tree[T]`, and both
nil and non-nil pointers.

A zero `Bytes{}` is invalid (empty path) rather than "nothing". A nil `File`
is the only "nothing".

### Asking what a File holds

Normalizing covers every File jenny receives, but two holes are left in code
users write against `File`, both of which compile:

- **A pointer case.** In a mapper, `case *jenny.Bytes:` or
  `f.(*jenny.Tree[T])` never matches, because jenny hands out only values.
- **A nil check in user code.** An author's own helper may compare a
  `jenny.File` that holds a nil pointer with nil, before jenny has seen it.

jenny closes them with three helpers, each of which treats a pointer exactly
as its value and a nil pointer as nil:

- `IsNil(f) bool`, rather than `f == nil`;
- `TreeOf[T](f) (T, bool)`, rather than `f.(jenny.Tree[T])`;
- `BytesOf(f) ([]byte, bool)`, rather than `f.(jenny.Bytes)`. It doesn't
  render a Tree; `Render` gets any File's bytes.

The rule, stated on `File`, in the package doc, the README and AGENTS.md, is
never to compare a `jenny.File` with nil, type-switch on one or type-assert
one. The examples and jenny's tests follow it.

The rule is checked by an analyzer, `jennylint`
(`github.com/sdboyer/jenny/jennylint`), which runs as a vet tool
(`go vet -vettool=$(which jennylint) ./...`) or as a golangci-lint module
plugin. Its diagnostics:

| Category | Reports | Why |
| --- | --- | --- |
| `pointer` | a case or assertion on a `jenny.File` naming `*jenny.Bytes` or `*jenny.Tree[T]` | it never matches: a bug |
| `value` | one naming `jenny.Bytes` or `jenny.Tree[T]` | it matches what jenny hands out, but misses a pointer from anywhere else; `BytesOf`/`TreeOf` match both |
| `nil` | `==`/`!=` with nil, or `case nil`, on a `jenny.File` | it misses a nil pointer; suggests `IsNil`, with a fix for `==`/`!=` |

`value` is a separate diagnostic with its own message, because the code it
reports works wherever jenny normalized the File, unlike `pointer`. It's still
on by default: the helpers replace it one for one, and a single rule with no
exceptions is easier for people and agents to follow than "assertions are fine
in a FileMapper but not in a helper".

jennylint is a module of its own, so that `golang.org/x/tools` and
golangci-lint's plugin registry stay out of jenny's `go.mod`. It matches
jenny's types by import path rather than importing jenny, so it doesn't
depend on a jenny version. Code in package jenny isn't checked: it's where
`normalize` turns pointers into values. CI runs it over jenny's own module.

## Sealing (decision 1)

The prototype keeps `File` sealed: only jenny's own types implement it.

- **For sealing:** the framework can move and stamp any file and keep its kind,
  and it knows every kind there is (for `FS`, `Validate` and rendering).
  Sealing is also what makes the nil guarantee enforceable. With an open
  interface, users write their own implementations, perhaps with pointer
  receivers, where a nil pointer might even be meaningful. That brings back
  the trap with less help from the framework.
- **For opening it** (io/fs-style optional capabilities, as the #36 discussion
  leaned): a sealed `Tree[T]` carries exactly one representation. With open
  capability interfaces, like `GoAST() (*ast.File, error)` or a CUE
  equivalent, one artifact could offer several, for example an AST and a
  token stream, as long as it keeps a single authoritative form internally. A sealed design can add that later as
  a kind that holds a value implementing several capability interfaces,
  without opening `File` itself.
- Opening `File` later breaks nobody; sealing it later would.

**[Sam]**

## FileMapper across kinds

- **What a mapper sees:** a `FileMapper` gets the `File` itself, always a
  value, and asks its kind with `TreeOf` and `BytesOf`. Two helpers make the
  common cases typed:
  - `BytesMapper(fn)` renders any file and hands `fn` the bytes. The result
    is a `Bytes`.
  - `TreeMapper[T](fn)` hands `fn` the value of every `Tree[T]` and passes
    everything else through, except a file a `BytesMapper` rendered from a
    `Tree[T]` (decision 4).
- **Capability-aware mappers:** a mapper can keep the tree where there is one.
  `ExampleFileMapper`'s `goHeader` adds the header comment *to the AST* when
  the file is a `Tree[goSource]`, so the file stays a tree. Any other file
  gets the header in its bytes.
- **Ordering rule: tree work before byte work.** A `BytesMapper` turns a
  `Tree` into `Bytes`, which is a loss of capability, not of correctness. With
  left-to-right `Map`, and byte work like `header` outermost, that's the
  natural order. This is documented on `Tree`, `FileMapper` and in the README,
  and a `TreeMapper` that runs too late reports it (decision 4).
- **TreeMapper funcs return a new value.** A `Tree`'s value isn't copied as it
  passes through jennies, so changing a tree in place, such as an `*ast.File`
  from `go/ast` or `cue/ast`, would also change every other holder of it, such
  as a generator's cache or the input to an earlier `Map`. That's documented on `TreeMapper`. It can't be enforced
  cheaply, because spotting a change in place would mean a deep comparison.
  `astHeader` and `tsHeader` in the examples show how cheap a new value
  usually is: a shallow copy with new comments.
- **When rendering is forced:** only in a `BytesMapper`, in `FS.Add` (and so
  `GenerateFS`), or by calling `Render`. `Generate`, `Prefix`, `Move`,
  `Validate` and `TreeMapper` never render
  (`TestTreeIsNotRenderedUntilNeeded`, `TestMove`).
- **`Move`:** changes a file's path without rendering it, through the same
  `withPath` that `Prefix` uses. `ExampleJenny_With`'s `testsApart` uses it.

### A TreeMapper after a BytesMapper (decision 4)

**Decided: an error only when a file lost its tree.** The options were:

- pass the file through, which hides an ordering mistake;
- always an error, which breaks every pipeline that mixes kinds;
- an error only for a file that was a tree before byte work rendered it.

Sam chose the last. A `BytesMapper` that renders a `Tree[T]` records on the
resulting `Bytes` the type it was rendered from (`Tree[T]`, as a
`reflect.Type`). That's provenance, not a second copy of the content, so it
doesn't break the one-representation rule. The record survives later
`BytesMapper`s, `Move` and `Prefix`. A `TreeMapper[T]` that meets such a file
returns an error naming the file, the tree type, and that a byte mapper ran
before the tree mapper:

```
postprocessing of out/a.words from Mixed failed: out/a.words was rendered from a jenny.Tree[…words] by a BytesMapper before this TreeMapper ran: tree mappers must come before byte mappers
```

Everything else passes through as before: files that were never trees, trees
of another type left as trees, a `Tree[U]` rendered to bytes when the mapper
is a `TreeMapper[T]`, and files a raw mapper rebuilt from bytes with `NewFile`,
which are new content with no record. `TestTreeMapperAfterBytesMapper` covers
each.

`FS` renders trees in `Add` too, but it holds only bytes and keeps no record,
so a `TreeMapper` passed to `FS.Map` still passes every file through. Keeping
provenance in the `FS` would change what it stores for each file, which this
change leaves alone. **[Sam]** if `FS.Map` should report it too.

## FS storage and its io/fs view

AST files are rendered when they're added to an `FS`, in `FS.Add` (and so
`GenerateFS`), outside the FS's lock. The options:

- **When added (prototype).** `FS` is where output becomes concrete. Its io/fs
  view is bytes, render errors show up at `GenerateFS` with the path and the
  jenny, and each file renders once. The cost is that a `TreeMapper` passed to
  `FS.Map` does nothing (see decision 4), so tree work belongs in the jenny
  tree.
- **When read.** Every io/fs call (`Stat` needs a size) could fail in a new
  way, results would need caching under the lock, and `Write` and `Verify`
  render anyway.
- **On Write and Verify.** The io/fs view still needs bytes.

## Windows

Paths are still slash-separated io/fs names. They're validated by #37's
`checkPath`, and converted only at the OS boundary, by `filepath.Localize` in
`Write`. `Move` and `Prefix` join with `path`, never `filepath`.

## Migration

The change breaks the core type, and the compiler finds every site:

| Old | New |
| --- | --- |
| `func(I) (*jenny.File, error)` | `func(I) (jenny.File, error)` |
| `*jenny.NewFile(p, d)` in a `Files` literal | `jenny.NewFile(p, d)`, which returns `Bytes` |
| `f.RelativePath` | `f.Path()` |
| `f.Data` | `f.Render()`, or `jenny.BytesOf(f)` |
| `f.From` (field) | `f.From()` |
| `func(f File) (File, error) { f.Data = ...; return f, nil }` | `jenny.BytesMapper(func(f File, data []byte) ([]byte, error))` |
| a mapper that sets `f.RelativePath` | `jenny.Move(f, newPath)` |
| `File.Exists`, `File.ToFS` | removed: return nil for "nothing"; use `NewFS().Add(f)` |
| `&File{}` as "nothing" | nil |
| `f == nil` on a `jenny.File` | `jenny.IsNil(f)` |

Most generator funcs change only their return type, and returning a nil
`*Bytes` from a "maybe" helper keeps working. Every mapper that edits bytes
becomes a `BytesMapper`.
