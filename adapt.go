package jenny

import (
	"fmt"
	"reflect"
)

// AdaptFunc is the set of func signatures that can adapt inputs of type P to
// inputs of type A: one or many Ps in, one or many As out.
type AdaptFunc[P, A Input] interface {
	func(P) (A, error) | func(P) ([]A, error) | func(...P) (A, error) | func(...P) ([]A, error)
}

// Adapt returns a jenny that converts its inputs to type A with fn, then runs
// a [Pipeline] of the provided jennies over the results.
//
// The signature of fn determines how the provided jennies are run, exactly as
// a [GenFunc] signature determines how a jenny is called. A single-input fn is
// called once per input, and the pipeline runs once over the As from each
// call. A many-input fn is called once with all inputs, and the pipeline runs
// once over all the As.
//
// If A is a nilable type, nil As are dropped, so fn can also act as a filter.
// If fn produces no As, the pipeline does not run.
func (b Builder[P]) Adapt[A Input, F AdaptFunc[P, A]](fn F, js ...Jenny[A]) Jenny[P] {
	sub := Pipeline(js...)
	drop := dropNil[A]()

	adapted := func(as []A, err error) (Files, error) {
		if err != nil {
			return nil, fmt.Errorf("adapter failed: %w", err)
		}
		return sub.gen(drop(as)...)
	}

	var run func(...P) (Files, error)
	switch f := any(fn).(type) {
	case func(P) (A, error):
		run = b.each(func(in P) (Files, error) {
			a, err := f(in)
			return adapted([]A{a}, err)
		})
	case func(P) ([]A, error):
		run = b.each(func(in P) (Files, error) { return adapted(f(in)) })
	case func(...P) (A, error):
		run = func(in ...P) (Files, error) {
			a, err := f(in...)
			return adapted([]A{a}, err)
		}
	case func(...P) ([]A, error):
		run = func(in ...P) (Files, error) { return adapted(f(in...)) }
	default:
		panic(fmt.Sprintf("unreachable: %T is not an AdaptFunc", fn))
	}

	return newJenny(run)
}

// dropNil returns a func that removes nil values from a slice of A, or that
// returns the slice unchanged if A is not a nilable type.
func dropNil[A Input]() func([]A) []A {
	switch reflect.TypeFor[A]().Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return func(as []A) []A {
			out := make([]A, 0, len(as))
			for i := range as {
				// Elem of a pointer to the value, so that nil interfaces are
				// inspected as interfaces rather than as an invalid Value.
				if !reflect.ValueOf(&as[i]).Elem().IsNil() {
					out = append(out, as[i])
				}
			}
			return out
		}
	default:
		return func(as []A) []A { return as }
	}
}
