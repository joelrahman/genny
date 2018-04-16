package generic

import (
	"C"
)

// Type is the placeholder type that indicates a generic value.
// When genny is executed, variables of this type will be replaced with
// references to the specific types.
//      var GenericType generic.Type
type Type interface{}

// Number is the placehoder type that indiccates a generic numerical value.
// When genny is executed, variables of this type will be replaced with
// references to the specific types.
//      var GenericType generic.Number
type Number float64

type CType interface{}

type CNumber C.double
