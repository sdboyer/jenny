package codejen

import "reflect"

// A Jenny is a single codejen code generator.
//
// Each Jenny works with exactly one type of input to its code generation, as
// indicated by its I type parameter, which may be any. The type [Input] is used
// as an indicator to humans of the purpose of such type parameters.
//
// Each Jenny takes either one or many Inputs, and produces one or many
// output files. Jennies may also return nils to indicate zero outputs.
//
// It is a design tenet of codejen that, in code generation, good separation
// of concerns starts with keeping a single file to a single responsibility. Thus,
// where possible, most Jennies should aim for one input to one output.
//
// Unfortunately, Go's generic system does not (yet?) allow expression of the
// necessary abstraction over individual kinds of Jennies as part of the Jenny
// interface itself. As such, the actual, functional interface is split into four:
//
//   - [OneToOne]: one [Input] in, one [File] out
//   - [OneToMany]: one [Input] in, many [File]s out
//   - [ManyToOne]: many [Input]s in, one [File] out
//   - [ManyToMany]: many [Input]s in, many [File]s out
//
// All jennies will follow exactly one of these four interfaces.
type Jenny[I Input] interface {
	// JennyName returns the name of the generator.
	JennyName() string

	// if only the type system let us do something like this, the API surface of
	// this library would shrink to a quarter its current size. so much more crisp
	// OneToOne[I] | ManyToOne[I any] | OneToMany[I] | ManyToMany[I]
}

// NamedJenny includes just the JennyName method. We have to have this interface
// due to the limits on Go's type system.
type NamedJenny interface {
	JennyName() string
}

// Input is used in generic type parameters solely to indicate to
// human eyes that that type parameter is used to govern the type passed as input to
// a jenny's Generate method.
//
// Input is an alias for any, because the codejen framework takes no stance on
// what can be accepted as jenny inputs.
type Input = any

// This library was originally written with Jinspiration as the name instead of
// Input.
//
// It's preserved here because you, dear reader of source code, deserve to
// giggle today.
//
// type Jinspiration = any

type JennyS[I Input] interface {
	// JennyName() string
	~struct {
		jennyName func() string
		o2o       func(I) (*File, error)
	} | ~struct {
		jennyName func() string
		o2m       func(I) (Files, error)
	} | ~struct {
		jennyName func() string
		m2o       func(...I) (*File, error)
	} | ~struct {
		jennyName func() string
		m2m       func(...I) (Files, error)
	}
}

type o2o[I Input] struct {
	jennyName func() string
	o2o       func(I) (*File, error)
}

type o2m[I Input] struct {
	jennyName func() string
	o2m       func(I) (Files, error)
}

type m2o[I Input] struct {
	jennyName func() string
	m2o       func(...I) (*File, error)
}

type m2m[I Input] struct {
	jennyName func() string
	m2m       func(...I) (Files, error)
}

// generic type guards have to be written with a func which can have type constraints
func guard[I Input, J JennyS[I]](j J) bool { return true }

var (
	_ = guard[any](o2o[any]{})
	_ = guard[any](m2o[any]{})
	_ = guard[any](o2m[any]{})
	_ = guard[any](m2m[any]{})
)

func generate[I Input, J JennyS[I]](j J, in ...I) (Files, error) {
	switch jenny := reflect.New(reflect.TypeOf(j)).Elem().Interface().(type) {
	case o2o[I]:
		for _, item := range in {
			f, err := jenny.o2o(item)
			if procerr := jl.wrapinerr(obj, oneout(jenny, f, err)); procerr != nil {
				result = multierror.Append(result, procerr)
			}
		}
	case m2o[I]:
	case o2m[I]:
		for _, item := range in {
			f, err := jenny.o2o(item)
			if procerr := jl.wrapinerr(obj, oneout(jenny, f, err)); procerr != nil {
				result = multierror.Append(result, procerr)
			}
		}
	case m2m[I]:
	}
}

func Generate[I Input, J JennyS[I]](j J, in ...I) (Files, error) {

}

func GenerateFS[I Input, J JennyS[I]](j J, in ...I) (FS, error) {

}

// Seems like we can't do this - this is actually something internal and invisible to the framework
// declared within each Jenny
func Decorate[I Input, J JennyS[I]](jenny J) J {

}

// Adapt allows a jenny designed for one type of Input to work with another type of Input, given
// a func that transforms the adapted Input to the original Input.
//
// takes a jenny that accepts a To input and
func Adapt[OuterI Input, InnerI Input, Outer JennyS[OuterI], Inner JennyS[InnerI]](j Inner, fn func(OuterI) InnerI) Outer {

}

func MapFiles[I Input, J JennyS[I]](j J, fns ...FileMapper) J {}

// Delineating input arity is important because it lets each jenny be truly only
// focused on its narrow scope - no potential for cross-info leak from one item
// to another. Can know that a priori
//
// Delineating output arity is important because...??

// var _ JennyS[any] = OTO[any]{}

// func (j OTO[Input]) JennyName() string {
// 	return "foo"
// }

// func adapt[InI Input, OutI Input, InIJ JennyS[InI], OutIJ JennyS[OutI]](j InIJ, fn func(InI) OutI) OutIJ {
//
// }

