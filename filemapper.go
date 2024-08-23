package codejen

import "reflect"

// FileMapper takes a File and transforms it into a new File.
//
// codejen generally assumes that FileMappers will reuse an
// unmodified byte slice.
type FileMapper func(File) (File, error)

func PostProcess[I Input](j Jenny[I], fns ...FileMapper) Jenny[I] {

}

func PostProcess2[I Input, J JennyS[I]](j J, fns ...FileMapper) J {
	switch jenny := reflect.New(reflect.TypeOf(j)).Elem().Interface().(type) {
	case o2o[I]:
		return o2o{
			jennyName: jenny.jennyName,
			o2o: func(in I) (*File, error) {

			},
		}
	case m2o[I]:
	case o2m[I]:
	case m2m[I]:
	}
}

func wrapo2o[I Input]()

// func PathingJenny[I Input]
