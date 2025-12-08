package structutil

import (
	"testing"
)

func TestApply(t *testing.T) {
	type HappyPath struct {
		Field string `validate:"required"`
	}

	type ValidationFailure struct {
		Field string `validate:"required"`
	}

	type Modification struct {
		Field string `mod:"default=default_value"`
	}

	tests := []struct {
		name    string
		input   any
		wantErr bool
		check   func(t *testing.T, input any)
	}{
		{
			name:    "happy path",
			input:   &HappyPath{Field: "value"},
			wantErr: false,
		},
		{
			name:    "validation failure",
			input:   &ValidationFailure{},
			wantErr: true,
		},
		{
			name:    "modification",
			input:   &Modification{},
			wantErr: false,
			check: func(t *testing.T, input any) {
				s := input.(*Modification)
				if s.Field != "default_value" {
					t.Errorf("Apply() did not set default value, got %v, want %v", s.Field, "default_value")
				}
			},
		},
		{
			name:    "not a pointer",
			input:   HappyPath{Field: "value"},
			wantErr: true,
		},
		{
			name:    "nil pointer",
			input:   (*HappyPath)(nil),
			wantErr: true,
		},
		{
			name:    "pointer to not a struct",
			input:   new(int),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Apply(tt.input); (err != nil) != tt.wantErr {
				t.Errorf("Apply() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, tt.input)
			}
		})
	}
}
