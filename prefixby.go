package jenny

import (
	"errors"
	"fmt"
	"path"
	"reflect"
)

// PrefixBy returns a [Pipeline] of the provided jennies that runs once per
// input, with the directory dir returns for that input prepended to the path
// of every File generated from it. It is [Jenny.Prefix] with a directory that
// depends on the input.
//
// dir's func shape decides how the pipeline runs, as an [AdaptFunc]'s does:
// dir takes one input, so the pipeline runs once per input, and each File comes
// from exactly one input. A many-input jenny in the pipeline is therefore
// called once per input, with that input alone, and makes one File per
// directory. A many-input jenny meant to see every input belongs beside
// PrefixBy, not inside it.
//
// The directory follows the same path rules as Prefix: it must be relative,
// slash-separated and portable (per [File.Validate]) once cleaned with
// [path.Clean]. A directory that doesn't is an error for that input, labeled
// with the input's name given [InputName]. An empty directory, or one that
// cleans to ".", means no prefix, as it does for Prefix. Files from different
// inputs share one path namespace, so two inputs that place a File at the same
// path are an error, per [Files.Validate].
//
// PrefixBy sees the inputs it's given. Inside a [Builder.Adapt] subtree, those
// are the adapted inputs; to prefix by the parent input, apply a PrefixBy from
// the parent's Builder outside the Adapt.
func (b Builder[I]) PrefixBy(dir func(I) string, js ...Jenny[I]) Jenny[I] {
	sub := Pipeline(js...)
	return newJenny(func(in ...I) (Files, error) {
		var (
			all  Files
			errs []error
		)
		for _, item := range in {
			d := path.Clean(dir(item))
			if d != "." {
				if err := checkPath(d); err != nil {
					err = fmt.Errorf("PrefixBy: %w", err)
					if b.inputName != nil {
						err = fmt.Errorf("%w for input %q", err, b.inputName(item))
					}
					errs = append(errs, err)
					continue
				}
			}
			fl, err := sub.Prefix(d).gen(item)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			all = append(all, fl...)
		}
		if len(errs) != 0 {
			return nil, errors.Join(errs...)
		}
		return all, all.Validate()
	}, &node{op: opPrefixBy, in: reflect.TypeFor[I](), fn: funcSymbol(dir), children: describeAll(js)})
}
