// Package genericutil provides generics related utility functions.
package genericutil

import "fmt"

// Must takes 2 arguments, the second being an error.
// If err is not nil, Must panics. Else the first argument is returned.
//
// Useful when inputs to some function are provided in the source code,
// and you are sure they are valid (if not, it's OK to panic).
// For example:
//
//	t := Must(time.Parse("2006-01-02", "2022-04-20"))
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

// First returns the first argument.
// Useful when you want to use the first result of a function call that has more than one return values
// (e.g. in a composite literal or in a condition).
//
// For example:
//
//	func f() (i, j, k int, s string, f float64) { return }
//
//	p := image.Point{
//	    X: First(f()),
//	}
func First[T any](first T, _ ...any) T {
	return first
}

func Second[T, V any](first T, second V, _ ...any) V {
	return second
}
