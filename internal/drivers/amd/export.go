package amd

// export.go — the package's internals that the dry run needs to spell out an
// exact write. Not an interface for anything else.

// EncodeCOValue returns the SMU argument a Curve Optimizer offset encodes to,
// so a dry run can print the exact payload SetCurveOptimizer would send.
func EncodeCOValue(offset int) uint32 { return encodeCOValue(offset) }
