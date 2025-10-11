// Package structs contains useful functions when dealing with structs.
package structs

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-playground/mold/v4/modifiers"
	"github.com/go-playground/validator/v10"
)

// Apply processes the tags on given object. It will apply any configured
// defaults and validation.
func Apply(config any) error {
	// Verify that the config is a non-nil pointer to a struct.
	val := reflect.ValueOf(config)
	if val.Kind() != reflect.Ptr {
		return fmt.Errorf("input must be a pointer to a struct, but got type %T", config)
	}
	if val.IsNil() {
		return fmt.Errorf("input pointer cannot be nil")
	}
	if val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("input must be a pointer to a struct, but got a pointer to %v", val.Elem().Kind())
	}

	// Apply struct modifications.
	t := modifiers.New()
	if err := t.Struct(context.Background(), config); err != nil {
		return fmt.Errorf("failed to modify struct: %w", err)
	}

	// Enforce validation constraints.
	v := validator.New()
	if err := v.Struct(config); err != nil {
		return fmt.Errorf("failed to validate struct: %w", err)
	}

	return nil
}
