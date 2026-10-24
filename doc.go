// Package jenny is a framework for building modular code generators out of
// small, single-purpose generator funcs, and for writing their output to disk
// or verifying that output already on disk is up to date.
//
// # One jenny, one purpose
//
// Code generators tend to become spaghetti. Without a rule for where new
// output belongs, one generator grows to emit several kinds of file. It
// sprouts knobs for each new variant and fills up with conditionals that check
// them, and outputs start reaching into each other's code. jenny is built on one
// constraint: a jenny maps an input type to a file with a single functional
// purpose. Go types for a resource are one jenny. A list of endpoints that
// references those types is another, a many-input jenny over the same inputs.
// A jenny that produces several files is ordinarily an aggregator.
//
// Generator funcs don't call each other. Each jenny takes on one
// responsibility, and jennies are combined through the composition operations
// below. Anything that operates on another jenny's output, such as adding a
// header, is itself composed in through those operations. The generator funcs themselves are ordinary Go, written however you
// like.
//
// The constraint is enforced by the language, not by convention. A jenny's
// signature is inputs in and files out, and its func never sees what other
// jennies produce. Where a hand-rolled generator would add a knob and a
// conditional, a jenny-based one usually adds a jenny. That doesn't guarantee
// cleanly defined responsibilities, but it tends to produce them.
//
// # Generator funcs and jennies
//
// A generator func takes one or many inputs and produces one or many files
// ([File]). Any of the four shapes in [GenFunc] will do:
//
//	func(I) (*File, error)    // one input, one file
//	func(I) (Files, error)    // one input, many files
//	func(...I) (*File, error) // many inputs, one file
//	func(...I) (Files, error) // many inputs, many files
//
// Returning nil (or a File that doesn't [File.Exists], or an empty Files)
// means "nothing to generate for this input". Single-input funcs are called
// once per input; many-input funcs are called once with all inputs.
//
// [Builder.New] turns a generator func into a [Jenny], which is a generator
// func with a name. Every jenny has the same type regardless of the shape of
// the func it was made from, so everything that works with jennies is written
// once and works with all of them. A [Builder] fixes the input type, so it's
// the only place a type argument is written:
//
//	kind := jenny.For[Kind]()
//	goTypes := kind.New("GoTypes", genGoTypes)
//
// The framework handles what each generator would otherwise repeat: it wraps
// errors with the jenny's name (and the input's, given [InputName]), drops
// empty outputs, records the jenny's name in each File's [File.From], and
// validates paths and path uniqueness.
//
// # Composition
//
// [Pipeline] runs a list of jennies that take the same input type, over the
// same inputs, into one path namespace. A pipeline is itself a Jenny, so
// pipelines nest.
//
// Methods decorate a single jenny, applied left to right in the order they
// read:
//
//   - [Jenny.Prefix] places its files under a directory.
//   - [Jenny.Map] passes its files through a [FileMapper], e.g. to add a header.
//   - [Jenny.With] applies wrappers defined outside this package.
//
// The functions [Prefix] and [Map] do the same for a group of jennies.
//
// [Builder.Adapt] runs a pipeline of jennies of one input type from within a
// pipeline of another, given a func that converts between them. Like a
// generator func, the adapter func's shape determines how it's called: a
// single-input adapter converts and runs its pipeline once per input; a
// many-input adapter converts all inputs and runs its pipeline once. Jennies
// in an adapted pipeline behave exactly as they would at the top level.
//
//	gen := jenny.Map(header,
//		group.New("GroupDocs", genGroupDocs).Prefix("docs"),
//		group.Adapt(splitGroup, // func(Group) ([]Type, error)
//			typ.New("GoTypes", genGoTypes).Prefix("go"),
//			typ.New("TypeIndex", genTypeIndex), // func(...Type) (*File, error)
//		),
//	)
//
// # Output
//
// [Jenny.GenerateFS] runs a jenny and collects its output into an [FS], which
// can [FS.Write] the files beneath a directory, or [FS.Verify] that the files
// beneath a directory match. Generators conventionally do the latter in CI,
// so that checked-in generated code can't drift from its inputs.
//
// Jenny is the successor to github.com/grafana/codejen.
package jenny
