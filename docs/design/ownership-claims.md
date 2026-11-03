# Ownership claims

Status: draft design, with a working prototype on `design/ownership-claims`.
This is stacked on `design/file-interface` (#38), and is the orphan-detection
item of #15.

jenny pipelines are stateless. When a run finds a file on disk that it didn't
generate, it can't tell whether the file belongs to the generator and is
stale (typically left behind when an input went away), or was never the
generator's. A **claim** settles that. A jenny says "the files here matching
`*.go` are mine", and `Verify` reports every file a claim matches but no
jenny generated this run.

Ownership is declared by the jenny, not the pipeline. A pipeline-level
`.Owns(glob)` was rejected because "it puts responsibility on the pipeline,
rather than the individual jenny, for indicating file ownership". The claim is
placed "by the same generalized prefixing systems that affect the jenny
itself".

Questions marked **[Sam]** are open for Sam to decide. Each notes what the
prototype does in the meantime.

## The prototype's API

```go
api.New("GoTypes", goType, jenny.Owns("*.go"))   // New gains ...NewOption

type Claim struct{ ... }  // Path() string; From(); FromString(); Match(path) bool
func (Jenny[I]) Claims() []Claim
func (*FS) Claims() []Claim
func (*FS) Stale(ctx, disk fs.FS) ([]string, error)
type StaleError struct{ Path string; Claim Claim }  // from VerifyFS and Verify
```

## Where a claim is declared (decision 2)

There are two designs. Both have the jenny declare the claim, and both place
it with `Prefix`.

### A: a claim is a File the generator func returns

This was Sam's original suggestion, and the first version of #38 built it.
A generator returns `jenny.Files{jenny.NewFile(...), jenny.NewClaim("*.go")}`,
and the claim travels through `Prefix`, `Map`, `Adapt` and `Pipeline` with the
other files.

- **For:**
  - No change to `New`, and the claim is literally part of what the jenny's
    func returns.
  - A claim can depend on the inputs.
  - User wrappers carry it without knowing about it.
- **Against:**
  - **A jenny run with zero inputs returns no claim.** That covers removing
    the last resource, and an `Adapt` that filters everything out. That's
    exactly when stale files appear, and they go unreported.
  - A one-file jenny has to switch to the `Files` shape to claim anything.
  - A single-input jenny returns the same claim once per input, so identical
    claims have to be deduplicated.
  - A claim has no content, so `File` has to split into a `File` umbrella and
    a `Content` with `Render`. `FileMapper` then becomes
    `func(Content) (Content, error)`, and anything that iterates over
    `Generate` or `AsFiles` has to tell claims apart. The claim ends up shaping
    the interface designed for content in #38, which is what the review asked
    to avoid.

### B: a jenny-level option, `New(name, fn, jenny.Owns("*.go"))` (prototype)

Claims are a static property of the jenny, not part of its output. Each
combinator carries them:
- `New` stamps them with the jenny's name;
- `Prefix` places them;
- `Map` leaves them alone;
- `Pipeline` collects them from its jennies;
- `Adapt` takes them from its subtree;
- `With` wrappers built from these combinators carry them too.

`GenerateFS` puts them in the `FS`.

- **For:**
  - **A zero-input run still claims**, and so does an `Adapt` subtree that
    never runs (`TestClaimsThroughComposition`).
  - Each claim is declared once, with no repetition per input.
  - `File`, `FileMapper` and `AsFiles` stay exactly as #38 has them. Claims
    fit into the interface rather than reshaping it.
  - The generator func doesn't change: README step 7 adds `jenny.Owns("*.go")`
    to the existing `goType` jenny.
- **Against:**
  - A claim can't depend on the inputs. A pattern can still cover
    input-dependent names (`*/v*.ts`), but a jenny can't claim only what it saw.
  - `Generate` returns only files, so callers who build an `FS` by hand from
    `Generate` must add `Jenny.Claims()` themselves. `GenerateFS` does it for
    them.
  - The claim is declared at the `New` call, next to the jenny's name, rather
    than inside its func. That's still the jenny, not the pipeline: the claim
    lives on the `Jenny` value and goes wherever that jenny is composed.

The prototype builds **B**. Its advantage over A for the zero-input case is
decisive for orphan detection, and it leaves #38's interface alone. Both
designs are equally able to carry claims through decorators, since a claim is
placed by `Prefix` either way. **[Sam]**

## Overlap and other jennies' files (decision 3)

- **Prototype: claims are permissive.** Claims may overlap, including claims
  from different jennies, and a claim may match files that other jennies
  generate. A file is stale only if *no* jenny generated it, whichever claim
  matches it. In the README, `GoTypes` owns `pkg/api/*.go`, and `routes.go`
  from `Routes` sits inside that claim without being stale. Identical claims
  (same pattern, same jennies) collapse into one in an `FS`.
- **Alternative: exclusive claims.** Another jenny's file inside a claim is an
  error, and so is an overlap between claims from different jennies. That
  would catch two generators fighting over a directory, but:
  - it forbids the README's layout, where two jennies share `pkg/api`;
  - it needs a test of whether two globs can match the same path. That's
    decidable for `path.Match` patterns, but not trivial, and harder with
    `**`.

**[Sam]**

## The pattern language and recursion (decision 5)

Patterns use `path.Match` syntax, the same as `fs.Glob`: slash-separated on
every OS, with `\` escapes, and `*` and `?` never matching `/`. A claim stores
its literal leading directory separately from its glob. That has two effects:
- `Prefix("gen[1]")` never needs escaping;
- the walk starts at that directory and goes only as deep as the glob has
  elements.

Recursion (`**`) has three options:

- **None (prototype).** A jenny that writes a deep tree claims each level, for
  example `*/types.go`.
- **Full `**`.** jenny would need its own matcher, since the standard library
  has none, with all the edge cases doublestar patterns bring. The walk would
  be unbounded below the literal directory, and the exclusive-overlap test
  above would get much harder.
- **A trailing `dir/**` only**, meaning everything beneath a directory. It's a
  prefix test, cheap to implement and to walk, and it covers the common case
  of a directory that is entirely generated.

**[Sam]**

## Verify, Write and deletion (decision 6)

- **`VerifyFS` and `Verify`** walk each claim's directory in the persisted
  `fs.FS` with `fs.WalkDir`, after comparing files. They report each stale
  file as a `*StaleError`, after the `*DriftError`s, in path order, all in
  #37's flat `errors.Join`.
- **`StaleError` is a sibling of `DriftError`, not a kind of it.**
  - A `DriftError` is about a generated file and its persisted copy, and its
    fields (`Missing`, `Diff`) describe how that copy is missing or differs.
    A stale file is the reverse: a persisted file with no generated
    counterpart.
  - Folding it in would add a third mode to a struct whose fields only make
    sense in their own mode. Existing `errors.As(&DriftError)` consumers that
    branch on `Missing` would misreport a stale file as "differs".
  - A sibling type is purely additive: consumers that don't know about it
    still get every `DriftError` they got before.
  - The alternative is a single `DriftError` with a `Stale` mode. That's one
    type for everything `Verify` reports, at the cost of changing what #37
    just published.

  **[Sam]**
- **`Write` never deletes.** Deleting is destructive and hard to reverse, and
  a claim that's too broad (`*.go` in a directory that also holds hand-written
  code) would destroy work. The options:
  - **Never (prototype).** `FS.Stale` lists stale files, and callers delete
    them if they want to.
  - **Opt-in**, e.g. `Write(ctx, root, jenny.Prune())`.
  - **A separate method**, `FS.Prune(ctx, root) ([]string, error)`. Its name
    says what it does, and it can run after `Verify` has shown what it would
    remove.

  None of them makes deletion the default. **[Sam]**
- **Scope of "generated".** Stale is judged against the `FS` that `Verify` is
  called on. Verifying only part of a generator's output against a root that
  another part also writes to would report that other part's files as stale.

## Case

- `Claim.Match` lowercases both the pattern and the path.
- Whether a file counts as generated is also decided ignoring case. That
  matters for safety: on a case-insensitive filesystem, writing `team.go` over
  an existing `Team.go` keeps the name `Team.go`. A case-sensitive check would
  call `Team.go` stale, and a delete would then remove the file just written.
- Lowercasing a pattern changes the meaning of an odd character range such as
  `[Z-a]`. That's accepted.

## Windows

- Claim patterns and file paths are slash paths, matched with `path.Match` and
  `strings`, never `filepath`. A claim's directory follows #37's portable path
  rules (`checkPath`), so for example `nul/*.go` is rejected on every OS.
- `Stale` and `VerifyFS` walk with `fs.WalkDir` over an `fs.FS`. `Verify`
  passes `os.DirFS(prefix)`, so every path compared is a slash path on every
  OS.
- `TestVerifyReportsStale` runs `Write`, `Stale` and `Verify` against a real
  temp directory, on all three CI operating systems.

## Migration

The change is additive on top of #38:
- `New` gains variadic options, so existing calls compile unchanged.
- `Jenny.Claims`, `FS.Claims`, `FS.Stale`, `Owns`, `Claim` and `StaleError`
  are new.
- `FS.Len` and `AsFiles` still count and return only files.
- The one behavioural change: once a jenny declares a claim, `VerifyFS` can
  return `*StaleError`s alongside `*DriftError`s. A consumer that assumes
  every joined error is a `DriftError` must handle the new type.
