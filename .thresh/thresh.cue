package thresh

// Thresh annotations for jenny. Data only: the #Seam schema lives in the
// thresh binary and validates this file at load (no import needed).

// Every Jenny is constructed by newJenny. Constructing one elsewhere bypasses
// the single place that guarantees a Jenny's run func shape.
//
// NOTE: as of thresh 744ad89, choke-point only matches non-generic composite
// literals (T{}), so Jenny[I]{} is not yet detected.
seams: "jenny-ctor": {
	kind:   "choke-point"
	paths:  ["*.go"]
	type:   "Jenny"
	onlyIn: ["newJenny"]
}
