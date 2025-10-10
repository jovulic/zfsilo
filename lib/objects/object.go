// Package objects contains useful functions when dealing with objects.
package objects

import (
	"context"
	"fmt"

	"github.com/go-playground/mold/v4/modifiers"
	"github.com/go-playground/validator/v10"
)

// Apply processes the tags on given object. It will apply any configured
// defaults and validation.
func Apply(config any) error {
	t := modifiers.New()
	if err := t.Struct(context.Background(), &config); err != nil {
		return fmt.Errorf("failed to modify struct: %w", err)
	}

	v := validator.New()
	if err := v.Struct(&config); err != nil {
		return fmt.Errorf("failed to validate struct: %w", err)
	}

	return nil
}
