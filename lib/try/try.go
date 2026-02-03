// Package try provides automated do/undo capability for arbirary routines.
package try

import (
	"context"
	"errors"
	"fmt"
)

// UndoFunc is a function that reverts a previous action.
type UndoFunc func(ctx context.Context) error

type UndoStack struct {
	undos []UndoFunc
}

func (s *UndoStack) Push(fn UndoFunc) {
	s.undos = append(s.undos, fn)
}

// Undo runs the stack in LIFO order.
func (s *UndoStack) Undo(ctx context.Context) error {
	var errs []error
	for i := len(s.undos) - 1; i >= 0; i-- {
		if err := s.undos[i](ctx); err != nil {
			errs = append(errs, fmt.Errorf("cleanup failed: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Do executes a function. If the function returns an error, Do automatically
// runs the undo on the  stack and combines errors.
func Do(ctx context.Context, fn func(*UndoStack) error) error {
	var stack UndoStack

	// Run the business logic
	err := fn(&stack)
	if err == nil {
		return nil
	}

	// If error, run the cleanup and combine the original error with any cleanup
	// error.
	cleanupErr := stack.Undo(ctx)
	return errors.Join(err, cleanupErr)
}
