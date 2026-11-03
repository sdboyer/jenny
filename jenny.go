package jenny

import (
	"errors"
	"fmt"
)

// Input is used in generic type parameters solely to indicate to
// human eyes that this type parameter specifies the type passed as input to
// a jenny.
//
// Input is an alias for any, because this framework takes no stance on
// what can be accepted as jenny inputs.
type Input = any

// When this library was first written, the type 'Input' was named 'Jinspiration'.
//
// It's preserved here because you, dear reader of source code, deserve to
// giggle today.
//
// type Jinspiration = any

// GenFunc is the set of generator func signatures from which a [Jenny] can be
// made: one or many inputs in, one or many [File]s out.
//
// A generator func returns a nil File or an empty Files to indicate it had
// nothing to generate for its input(s). A nil pointer, such as a nil *Bytes
// returned as a File, is treated exactly as a nil File, here and wherever else
// jenny receives Files. Generator funcs do not need to set [File.From]; it is
// set for them.
type GenFunc[I Input] interface {
	func(I) (File, error) | func(I) (Files, error) | func(...I) (File, error) | func(...I) (Files, error)
}

// A Jenny is a single code generator, or a composition of them, that takes
// inputs of type I and produces [Files].
//
// Jennies are made from a [GenFunc] with [Builder.New], combined with
// [Pipeline], adapted to another input type with [Builder.Adapt], and
// decorated with [Jenny.Prefix], [Jenny.Map] and [Jenny.With]. Whatever the
// arity of the GenFunc it was made from, every Jenny is used the same way.
//
// A Jenny is never invoked with zero inputs; it generates nothing instead.
// Its [Claim]s don't depend on its inputs, so it has them even then.
//
// The zero value of Jenny generates nothing, and claims nothing.
type Jenny[I Input] struct {
	run func(in ...I) (Files, error)

	// claims are the jenny's claims, placed as its files are.
	claims []Claim
}

// newJenny is the only place a Jenny is constructed.
func newJenny[I Input](run func(in ...I) (Files, error), claims []Claim) Jenny[I] {
	return Jenny[I]{run: run, claims: claims}
}

func (j Jenny[I]) gen(in ...I) (Files, error) {
	if j.run == nil || len(in) == 0 {
		return nil, nil
	}
	return j.run(in...)
}

// Generate runs the jenny over the provided inputs and returns the resulting
// Files, which are guaranteed to meet the invariants of [Files.Validate].
func (j Jenny[I]) Generate(in ...I) (Files, error) {
	return j.gen(in...)
}

// GenerateFS is like [Jenny.Generate], but returns the results as an [FS],
// which also holds the jenny's [Jenny.Claims].
func (j Jenny[I]) GenerateFS(in ...I) (*FS, error) {
	fl, err := j.gen(in...)
	if err != nil {
		return nil, err
	}
	jfs := NewFS()
	if err := jfs.Add(fl...); err != nil {
		return nil, err
	}
	if err := jfs.addClaims(j.claims...); err != nil {
		return nil, err
	}
	return jfs, nil
}

// Claims returns the jenny's claims, declared with [Owns] on the jennies it's
// made of, and placed as their files are.
func (j Jenny[I]) Claims() []Claim {
	return append([]Claim(nil), j.claims...)
}

// Prefix returns a jenny that prepends dir to the path of every File the
// receiver generates, and to the pattern of every one of its claims.
func (j Jenny[I]) Prefix(dir string) Jenny[I] {
	claims := make([]Claim, len(j.claims))
	for i, c := range j.claims {
		claims[i] = c.placed(dir)
	}
	return newJenny(func(in ...I) (Files, error) {
		fl, err := j.gen(in...)
		if err != nil {
			return nil, err
		}
		out := make(Files, len(fl))
		for i, f := range fl {
			out[i] = f.withPath(prefixer(dir))
		}
		return out, out.Validate()
	}, claims)
}

// Map returns a jenny that passes every File the receiver generates through fm.
func (j Jenny[I]) Map(fm FileMapper) Jenny[I] {
	return newJenny(func(in ...I) (Files, error) {
		fl, err := j.gen(in...)
		if err != nil {
			return nil, err
		}
		out, err := mapFiles(fl, fm)
		if err != nil {
			return nil, err
		}
		return out, out.Validate()
	}, j.claims)
}

// mapFiles passes each File through fm. A FileMapper can't drop a file, so a
// nil File from fm, including a nil pointer, is an error.
func mapFiles(fl []File, fm FileMapper) (Files, error) {
	out := make(Files, len(fl))
	for i, f := range fl {
		nf, err := fm(f)
		if err != nil {
			return nil, fmt.Errorf("postprocessing of %s from %s failed: %w", f.Path(), jennystack(f.From()), err)
		}
		if nf = normalize(nf); nf == nil {
			return nil, fmt.Errorf("postprocessing of %s from %s returned a nil File", f.Path(), jennystack(f.From()))
		}
		out[i] = nf
	}
	return out, nil
}

// With applies each of the provided wrappers to the receiver, in order. It is
// the hook for wrappers defined outside this package.
func (j Jenny[I]) With(wrappers ...func(Jenny[I]) Jenny[I]) Jenny[I] {
	for _, w := range wrappers {
		j = w(j)
	}
	return j
}

// Builder makes jennies that take inputs of type I. Create one with [For].
type Builder[I Input] struct {
	inputName func(I) string
}

// Option configures a [Builder].
type Option[I Input] func(*Builder[I])

// InputName sets a func that derives a human-readable name for an input. When
// set, errors from single-input jennies made by the Builder identify the input
// that caused them.
func InputName[I Input](fn func(I) string) Option[I] {
	return func(b *Builder[I]) {
		b.inputName = fn
	}
}

// For returns a Builder for jennies that take inputs of type I.
func For[I Input](opts ...Option[I]) Builder[I] {
	var b Builder[I]
	for _, opt := range opts {
		opt(&b)
	}
	return b
}

// each lifts a single-input func to a many-input func, calling fn once per
// input and labeling errors with the input's name.
func (b Builder[I]) each(fn func(I) (Files, error)) func(...I) (Files, error) {
	return func(in ...I) (Files, error) {
		var (
			all  Files
			errs []error
		)
		for _, item := range in {
			fl, err := fn(item)
			if err != nil {
				if b.inputName != nil {
					err = fmt.Errorf("%w for input %q", err, b.inputName(item))
				}
				errs = append(errs, err)
				continue
			}
			all = append(all, fl...)
		}
		if len(errs) != 0 {
			return nil, errors.Join(errs...)
		}
		return all, nil
	}
}

// New makes a named Jenny from a generator func of any [GenFunc] arity.
//
// The name identifies the jenny in errors and in the [File.From] of every
// File it generates, and of every [Claim] declared with [Owns].
func (b Builder[I]) New[F GenFunc[I]](name string, fn F, opts ...NewOption) Jenny[I] {
	if name == "" {
		panic("jenny: New requires a non-empty name")
	}
	nj := namedJenny(name)

	var cfg newConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	claims := make([]Claim, len(cfg.owns))
	for i, p := range cfg.owns {
		claims[i] = newClaim(p, nj)
	}

	named := func(fl Files, err error) (Files, error) {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		// Stamp copies: a gen func may return a slice it holds on to (e.g. a
		// cache), which must not accumulate a stamp per call.
		out := make(Files, len(fl))
		for i, f := range fl {
			// This is where Files enter from generator funcs, so it's where a
			// nil pointer becomes a nil File.
			if f = normalize(f); f != nil { // a nil is left for Validate to report
				f = f.stamped(nj)
			}
			out[i] = f
		}
		return out, nil
	}

	var run func(...I) (Files, error)
	switch f := any(fn).(type) {
	case func(I) (File, error):
		run = b.each(func(in I) (Files, error) { return named(single(f(in))) })
	case func(I) (Files, error):
		run = b.each(func(in I) (Files, error) { return named(f(in)) })
	case func(...I) (File, error):
		run = func(in ...I) (Files, error) { return named(single(f(in...))) }
	case func(...I) (Files, error):
		run = func(in ...I) (Files, error) { return named(f(in...)) }
	default:
		panic(fmt.Sprintf("unreachable: %T is not a GenFunc", fn))
	}

	return newJenny(func(in ...I) (Files, error) {
		fl, err := run(in...)
		if err != nil {
			return nil, err
		}
		if err := fl.Validate(); err != nil {
			return nil, fmt.Errorf("%s returned invalid Files: %w", name, err)
		}
		return fl, nil
	}, claims)
}

func single(f File, err error) (Files, error) {
	if err != nil || normalize(f) == nil {
		return nil, err
	}
	return Files{f}, nil
}

// NamedJenny is a named element of a [File.From] stack.
type NamedJenny interface {
	JennyName() string
}

type namedJenny string

func (n namedJenny) JennyName() string { return string(n) }
