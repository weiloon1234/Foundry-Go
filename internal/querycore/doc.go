// Package querycore owns the model-independent SQL planning and compiler implementation.
// Public ORM declarations enforce concrete model, field and result ownership before
// lowering through validated framework capabilities. Keep this package independent
// of public ORM facades and application assembly.
package querycore
