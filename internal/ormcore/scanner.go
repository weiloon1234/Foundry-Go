package ormcore

// ScannerFunc adapts an existing driver-boundary scan callback to sql.Scanner.
// Domain value types stay in the callback's typed destination closure.
type ScannerFunc func(any) error

// Scan delegates one raw driver value to the retained callback.
func (scan ScannerFunc) Scan(source any) error { return scan(source) }
