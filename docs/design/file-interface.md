# File as an interface

Status: draft design, with a working prototype on `design/file-interface`.

`File` changes from a struct to a sealed interface. There are two reasons:

1. **Ownership claims.** jenny pipelines are stateless, so a run can't tell
   whether a file on disk that nothing generated this time is the generator's
   and stale, or was never the generator's. A jenny can now return a `Claim`
   among its files, meaning "files here matching `*.go` are mine". `Prefix`
   places a claim exactly as it places the jenny's files, and `Verify` reports
   a file on disk as stale when a claim matches it and no jenny generated it.
   Ownership stays with the jenny, not the pipeline, which is why a
   pipeline-level `.Owns(glob)` was rejected.
2. **AST-backed files.** A file can be carried as a value, such as an
   `*ast.File`, and rendered to bytes only when something needs bytes. Mappers
   such as gofmt, header insertion or import fixing can work on the tree.

Questions marked **[Sam]** are left for Sam to decide. Each of them notes what
the prototype does for now.

## The prototype's API

```go
type File interface {
    Path() string        // slash path; for a Claim, its pattern
    From() []NamedJenny
    FromString() string
    Validate() error
    placed(dir string) File     // sealed: lets Prefix place any File
    stamped(nj NamedJenny) File // sealed: lets New stamp any File
}

type Content interface { File; Render() ([]byte, error) }

type Bytes struct{ ... }   // NewFile(path, data, from...) Bytes; Data() []byte
type Tree[T any] struct{ ... } // NewTree(path, value, render, from...); Value() T
type Claim struct{ ... }   // NewClaim(pattern, from...); Match(path) bool

type FileMapper func(Content) (Content, error)
func BytesMapper(func(f File, data []byte) ([]byte, error)) FileMapper
func TreeMapper[T any](func(f File, value T) (T, error)) FileMapper

func (*FS) Stale(ctx, disk fs.FS) ([]string, error)
```

`GenFunc` shapes become `func(I) (File, error)` and so on, so a `*File` is now
a `File`.

## The interface's method set

**Path and provenance** (`Path`, `From`, `FromString`) are on every File,
because everything the framework does to a File without knowing what it is
(placing it, stamping it, reporting collisions and errors) needs only these.

**Content is not on `File`.** It's on `Content`, which embeds `File` and adds
`Render() ([]byte, error)`. If `Render` were on `File`, a claim would need a
`Render` that always fails, and every consumer would have to remember that.
With `Content`, the compiler knows the difference: a `FileMapper` takes a
`Content`, so it can never be handed a claim or return one.

**Sealed.** The prototype seals the interface with two unexported methods:
`placed` (for `Prefix`) and `stamped` (for `New` to add itself to `From`).

- *Sealed (prototype):* the framework can rewrite path and provenance on any
  File and get back the same concrete type. So a `TreeMapper` that comes after
  `Prefix` still finds its `Tree[T]`. Write, Verify and Validate know every
  kind there is. `Tree[T]` is generic, so content can still be any
  representation; what callers can't add is new *behaviour* (another kind
  besides content and claim).
- *Open:* users can define their own kinds. But the framework can only move or
  stamp one by wrapping it, for example `prefixed{File, dir}`, which hides the
  concrete type from later type assertions, or by requiring a `WithPath` method
  on every implementation. Every new kind also needs a rule from Write, Verify
  and Validate. **[Sam]** The prototype seals; opening the interface later
  doesn't break anyone, but sealing it later does.

**Is a claim a `File`, or a sibling type in the same slice?** The prototype
makes it a `File`, as Sam suggested. Both ways the claim travels in the
returned slice; the question is whether the shared interface is called `File`.

- *For:* no new return types and no new `GenFunc` shapes. `Prefix`, `Adapt`,
  `Pipeline`, `With` and user wrappers carry claims without changing their
  signatures, so a claim is placed by "the same generalized prefixing systems
  that affect the jenny itself".
- *Against:* "File" now means "output entry", and anything that walks `Files`
  (a user listing paths, `FS.AsFiles`) sees claims and has to tell them apart.
  The cost is limited because mappers are typed on `Content` and never see
  claims. A sibling design would keep `File` for content and add an umbrella
  type such as `Output`; it would rename more of the API for a little clarity.
- *Not a File at all* (a separate return value or channel): this changes every
  `GenFunc` shape and every decorator, which is exactly what Sam wanted to
  avoid.

**Where a claim is declared.** In the prototype, the generator func returns
the claim, as Sam described. That has two costs:

- A one-file jenny that wants to claim must switch to the `Files` shape, and a
  single-input jenny returns the same claim once per input. Identical claims
  are therefore redundant rather than conflicting, and `FS` keeps one of them.
- A jenny that runs with **zero inputs** returns no claim, so its stale files
  go unreported. Removing the last resource, or an `Adapt` that filters
  everything out, leaves stale files undetected.

An alternative is an option on `New`: `api.New("GoTypes", goType,
jenny.Owns("*.go"))`. The jenny still declares the claim, so it's not
pipeline-level, and it could emit the claim exactly once per run, even with
zero inputs. It sits on top of the same claim type. **[Sam]**

## FileMapper across kinds

- **What a mapper sees.** `FileMapper` is `func(Content) (Content, error)`, a
  general escape hatch that can type-switch on kinds. Two helpers make the
  common cases typed:
  - `BytesMapper(fn)` renders any `Content` and gives `fn` bytes; the result
    is `Bytes`. The README's `gofmt` and `header` are BytesMappers.
  - `TreeMapper[T](fn)` applies `fn` to the value of every `Tree[T]` and
    passes everything else through.
- **When rendering is forced.** Rendering happens only in a `BytesMapper`, in
  `FS.Add` (and so `GenerateFS`), or when you call `Render`. `Generate`,
  `Prefix`, `Validate` and `TreeMapper` never render
  (`TestTreeIsNotRenderedUntilNeeded`).
- **Ordering hazard.** A `TreeMapper` placed after a `BytesMapper` silently does
  nothing, because by then the tree has become bytes. The alternatives:
  - an error when a `TreeMapper` meets a non-`Tree` file, which breaks
    pipelines that mix Go trees with TypeScript bytes;
  - a `TreeMapper` that takes a parse func to lift bytes back into a tree.

  **[Sam]** The prototype passes the file through, and documents it.
- **Mappers never see claims.** Only `Prefix` places claims. That's the
  sound case, because placing a pattern under a directory is well-defined.
  Arbitrary path rewrites aren't: renaming `x.go` to `y.go` has no meaning
  when applied to `*.go`. A claim keeps its literal directory apart from its
  glob, so `Prefix("gen[1]")` never needs escaping.
- **Gap: moving a `Content` without rendering it.** `ExampleJenny_With`'s
  `testsApart` moves files whose names end in `_test.go`. With the interface
  sealed, moving one means rendering it and building a new `Bytes`, which
  loses a `Tree`. A `jenny.Move(c Content, path string) Content` would fix
  that. **[Sam]** It's not in the prototype.

## FS storage and its io/fs view

- **Claims are invisible through io/fs.** They aren't files. `FS` stores them
  apart from its `MapFS`, so `fstest.TestFS` and `Glob` see only files.
  `AsFiles` returns files *and* claims, so that `Merge` and `Map` carry claims
  along. `Len` counts files only.
- **AST files are rendered on add.** The prototype picks this option.
  - *On add (prototype):* `FS` is where output becomes concrete. Its io/fs
    view is bytes, render errors show up at `GenerateFS` with the path and the
    jenny, and each file renders once. The cost is that `FS.Map` with a
    `TreeMapper` does nothing, so tree work belongs in the jenny tree.
  - *On read:* every io/fs call (`Stat` needs a size) can now fail in a new
    way, results need caching under the lock, and `Write` and `Verify` render
    anyway.
  - *On Write and Verify:* the io/fs view would still need bytes.

## Validation

- **Files against files:** unchanged. Paths that are equal ignoring case
  collide.
- **Claims against claims:** never an error. Identical claims (same pattern,
  same `From`) collapse into one in `FS`. Overlapping claims, including claims
  from different jennies, are kept.
- **Claims against other jennies' files:** allowed. A file is stale only if
  *no* jenny generated it, whichever claim matches it. In the README, `GoTypes`
  claims `pkg/api/*.go`, and `routes.go` from `Routes` sits inside that claim
  without being stale.
- **The other option is exclusive claims:** a file from another jenny inside a
  claim is an error, and so are overlapping claims from different jennies.
  That would catch two generators fighting over a directory. The costs:
  - it forbids the README's layout, where two jennies share `pkg/api`;
  - it needs a test of whether two globs can match the same path, which is
    decidable for `path.Match` patterns but not trivial, and harder with
    `**`.

  **[Sam]** The prototype allows overlap.
- **Case:** `Claim.Match` lowercases both the pattern and the path. Whether a
  file counts as generated is also decided ignoring case. That matters for
  safety. On a case-insensitive filesystem, writing `team.go` over an existing
  `Team.go` keeps the name `Team.go`. A case-sensitive check would call
  `Team.go` stale, and a delete would then remove the file just written.
  Lowercasing a pattern can change the meaning of an odd character range such
  as `[Z-a]`; that's accepted.
- **Claim validity:**
  - the directory follows the same path rules as content paths;
  - the glob must be a valid `path.Match` pattern with no empty, `.` or `..`
    elements;
  - `From` must not be empty.

  A `Prefix` that places a claim outside the root is caught.
- **A zero `Bytes{}` is invalid, not "nothing".** A nil `File` is now the only
  way to say "nothing generated".

## The pattern language

Patterns use `path.Match` syntax, the same as `fs.Glob`: slash-separated on
every OS, `\` escapes, and `*` and `?` never match `/`. `NewClaim` splits the
pattern into a literal directory (the leading elements with no pattern
characters) and a glob. That lets the walk start at the directory and go only
as deep as the glob has elements.

**Recursion (`**`).** The prototype doesn't support it. The options:

- *None (prototype):* a jenny writing a deep tree claims each level, for
  example `*/types.go`.
- *Full `**`:* needs a matcher of jenny's own, since the standard library has
  none, with doublestar-style edge cases. It also needs an unbounded walk
  below the literal directory, and makes the exclusive-overlap check above
  much harder.
- *A trailing `dir/**` only* ("everything beneath here"): a prefix test, cheap
  to implement and to walk. It covers the common case of a fully generated
  directory. **[Sam]**

## Verify and Write with claims

- **Verify** now walks each claim's directory on disk after comparing the
  files, and adds one error per stale file, in path order:
  `pkg/api/team.go: stale: matches claim pkg/api/*.go of jenny "GoTypes", but was not generated`.
  The walk is in `FS.Stale(ctx, fs.FS)`, which takes any `fs.FS`. Verify
  passes it `os.DirFS(prefix)`.
- **Write never deletes.** It's unchanged. Deleting is destructive and hard to
  reverse, and a claim that's too broad (say, `*.go` in a directory that also
  holds hand-written code) would destroy work. The options:
  - *Never (prototype):* `Stale` lists the files, and callers delete them
    themselves if they want.
  - *Opt-in:* for example `Write(ctx, root, jenny.Prune())`.
  - *A separate method:* `FS.Prune(ctx, root) ([]string, error)`, which says
    what it does in its name and can be run after `Verify` has shown what it
    would remove.

  **[Sam]** Deletion is not the default in any of these.
- **Scope of "generated".** Stale is judged against the FS `Verify` is called
  on. Verifying only part of a generator's output against a root that
  another part also writes to would report that other part's files as stale.

### Reconciling with `feat/verifyfs-drift`

That branch, which adds `FS.VerifyFS`, typed `DriftError`s and a Windows path
audit, wasn't pushed when this was written. So the Verify changes here are
kept to a minimum:

- stale detection is a separate function over an `fs.FS`, which `VerifyFS`
  can call directly;
- stale entries are plain errors appended after the diffs, ready to become
  a `DriftError` kind;
- the path rules moved unchanged from `File.Validate` into `validatePath`,
  which will conflict textually with the audit but not in meaning.

## Windows

- Claims and file paths are slash paths. They're matched with `path.Match` and
  `strings` on slash paths, never with `filepath`.
- `Stale` walks with `fs.WalkDir` over an `fs.FS`, so the paths it compares
  are slash paths on every OS. Verify builds that `fs.FS` with
  `os.DirFS(prefix)`.
- OS paths appear only at the boundary: the `prefix` passed to `os.DirFS`, and
  `filepath.Join(prefix, filepath.FromSlash(p))` in error messages and writes.
- `TestVerifyReportsStale` runs `Write`, `Stale` and `Verify` against a real
  temp directory on all three CI operating systems.

## Migration

This is a breaking change to the core type. The compiler finds every site.

| Old | New |
| --- | --- |
| `func(I) (*jenny.File, error)` | `func(I) (jenny.File, error)` |
| `*jenny.NewFile(p, d)` in a `Files` literal | `jenny.NewFile(p, d)`, which returns `Bytes` |
| `f.RelativePath` | `f.Path()` |
| `f.Data` | `f.(jenny.Bytes).Data()`, or `Render()` on a `Content` |
| `f.From` (field) | `f.From()` |
| `func(f File) (File, error) { f.Data = ...; return f, nil }` | `jenny.BytesMapper(func(f File, data []byte) ([]byte, error))` |
| a mapper that sets `f.RelativePath` | render and `NewFile(newPath, data, f.From()...)`; see the `Move` gap |
| `File.Exists`, `File.ToFS` | removed: return nil for "nothing"; use `NewFS().Add(f)` |
| `&File{}` as "nothing" | nil |
| `FS.AsFiles` entries | may include claims |

Most generator funcs change only their return type: `*jenny.File` becomes
`jenny.File`. Every mapper changes. The main thing users have to work out is
claims showing up in `Files` they iterate over.
