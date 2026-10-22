package jenny

import "errors"

// Pipeline returns a jenny that runs each of the provided jennies, in order,
// over the same inputs.
//
// The Files from all the jennies share one relative path namespace. Pipeline
// does not modify paths, and it is an error for two Files to have the same
// path (per [Files.Validate]).
func Pipeline[I Input](js ...Jenny[I]) Jenny[I] {
	return newJenny(func(in ...I) (Files, error) {
		var (
			all  Files
			errs []error
		)
		for _, j := range js {
			fl, err := j.gen(in...)
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
	})
}

// Prefix returns a [Pipeline] of the provided jennies, with dir prepended to
// the path of every File they generate.
func Prefix[I Input](dir string, js ...Jenny[I]) Jenny[I] {
	return Pipeline(js...).Prefix(dir)
}

// Map returns a [Pipeline] of the provided jennies, with every File they
// generate passed through fm.
func Map[I Input](fm FileMapper, js ...Jenny[I]) Jenny[I] {
	return Pipeline(js...).Map(fm)
}
