# File as an interface

Status: draft design, with a working prototype on `design/file-interface`.
Ownership claims and stale-file detection (#15), which were part of an
earlier version of this proposal, are now a separate PR stacked on this one.

`File` changes from a struct to a sealed interface, so that a file's content
can travel as something other than bytes (#36). For example, a Go file can
travel as an `*ast.File`, so that mappers like gofmt, header insertion or
import fixing work on the tree, and it's rendered to bytes only when something
needs bytes. Every method on the interface is there because of the two content
kinds below.

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
  interface, with a zero `File{}` meaning "nothing" and accessors such as
  `jenny.TreeOf[T](f) (T, bool)` instead of type assertions. This removes the
  trap by construction, with no reflection. The cost is that `File` is no
  longer an interface. Mappers would switch on accessors instead of types, and
  users couldn't write `f.(jenny.Tree[T])`. It's a reasonable alternative to
  normalizing if the reflection check feels wrong. **[Sam]**

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
  capability interfaces, like `GoAST() (*ast.File, error)`, one artifact could
  offer several, for example an AST and a token stream, as long as it keeps a
  single authoritative form internally. A sealed design can add that later as
  a kind that holds a value implementing several capability interfaces,
  without opening `File` itself.
- Opening `File` later breaks nobody; sealing it later would.

**[Sam]**

## FileMapper across kinds

- **What a mapper sees:** a `FileMapper` gets the `File` itself, and can
  type-switch on its kind. Two helpers make the common cases typed:
  - `BytesMapper(fn)` renders any file and hands `fn` the bytes. The result
    is a `Bytes`.
  - `TreeMapper[T](fn)` hands `fn` the value of every `Tree[T]` and passes
    everything else through.
- **Capability-aware mappers:** a mapper can keep the tree where there is one.
  `ExampleFileMapper`'s `goHeader` adds the header comment *to the AST* when
  the file is a `Tree[goSource]`, so the file stays a tree. Any other file
  gets the header in its bytes.
- **Ordering rule: tree work before byte work.** A `BytesMapper` turns a
  `Tree` into `Bytes`, which is a loss of capability, not of correctness. With
  left-to-right `Map`, and byte work like `header` outermost, that's the
  natural order. This is documented on `Tree`, `FileMapper` and in the README.
- **TreeMapper funcs return a new value.** A `Tree`'s value isn't copied as it
  passes through jennies, so changing an `*ast.File` in place would also
  change every other holder of it, such as a generator's cache or the input to
  an earlier `Map`. That's documented on `TreeMapper`. It can't be enforced
  cheaply, because spotting a change in place would mean a deep comparison.
  `astHeader` in the example shows how cheap a new value usually is: a
  shallow copy of the `*ast.File` with new comments.
- **When rendering is forced:** only in a `BytesMapper`, in `FS.Add` (and so
  `GenerateFS`), or by calling `Render`. `Generate`, `Prefix`, `Move`,
  `Validate` and `TreeMapper` never render
  (`TestTreeIsNotRenderedUntilNeeded`, `TestMove`).
- **`Move`:** changes a file's path without rendering it, through the same
  `withPath` that `Prefix` uses. `ExampleJenny_With`'s `testsApart` uses it.

### A TreeMapper after a BytesMapper (decision 4)

The tree is gone by then, so the `TreeMapper` finds nothing to work on. The
options:

- **Pass the file through (prototype).** This is safe in pipelines that mix Go
  trees with TS bytes, but an ordering mistake goes unnoticed.
- **Always an error** (where the #36 discussion leaned): the mistake gets
  caught, but every pipeline that mixes kinds breaks.
- **An error only when a file has lost the capability** (from the review):
  `BytesMapper` records on the resulting `Bytes` that it was rendered from a
  `Tree[T]`, and a `TreeMapper[T]` errors on such a file. Files that were
  never trees still pass through. Recording where the bytes came from doesn't
  store a second copy of the content, so it doesn't break the
  one-representation rule. Not implemented.

**[Sam]**

## FS storage and its io/fs view

AST files are rendered when they're added to an `FS`, in `FS.Add` (and so
`GenerateFS`), outside the FS's lock. The options:

- **When added (prototype).** `FS` is where output becomes concrete. Its io/fs
  view is bytes, render errors show up at `GenerateFS` with the path and the
  jenny, and each file renders once. The cost is that a `TreeMapper` passed to
  `FS.Map` does nothing, so tree work belongs in the jenny tree.
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
| `f.Data` | `f.Render()`, or `f.(jenny.Bytes).Data()` |
| `f.From` (field) | `f.From()` |
| `func(f File) (File, error) { f.Data = ...; return f, nil }` | `jenny.BytesMapper(func(f File, data []byte) ([]byte, error))` |
| a mapper that sets `f.RelativePath` | `jenny.Move(f, newPath)` |
| `File.Exists`, `File.ToFS` | removed: return nil for "nothing"; use `NewFS().Add(f)` |
| `&File{}` as "nothing" | nil |

Most generator funcs change only their return type, and returning a nil
`*Bytes` from a "maybe" helper keeps working. Every mapper that edits bytes
becomes a `BytesMapper`.
