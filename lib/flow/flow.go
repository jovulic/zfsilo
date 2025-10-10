// Package flow contains useful functions and utilities to help with code flow.
package flow

import "fmt"

// Must takes 2 arguments, the second being an error.
// If err is not nil, Must panics. Else the first argument is returned.
func Must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("must: %v", err))
	}
	return v
}

// Apply executes the apply functions on the value.
func Apply[T any](value T, applyFns ...func(T)) {
	for _, fn := range applyFns {
		fn(value)
	}
}
