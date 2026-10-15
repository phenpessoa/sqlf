// Package builtins reports whether a name belongs to a function or type
// that ships with PostgreSQL in the pg_catalog schema.
package builtins

//go:generate sh gen.sh

// serialTypes are the pseudo-types that CREATE TABLE expands into a
// sequence. They are not rows of pg_type.
var serialTypes = map[string]struct{}{
	"bigserial":   {},
	"serial":      {},
	"serial2":     {},
	"serial4":     {},
	"serial8":     {},
	"smallserial": {},
}

// IsFunction reports whether name is a pg_catalog function.
func IsFunction(name string) bool {
	_, ok := functions[name]
	return ok
}

// IsType reports whether name is a pg_catalog type or a serial pseudo-type.
func IsType(name string) bool {
	if _, ok := types[name]; ok {
		return true
	}
	_, ok := serialTypes[name]
	return ok
}
