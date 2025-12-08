package stringutil_test

import (
	"testing"

	"github.com/jovulic/zfsilo/lib/stringutil"
)

func TestMultiline(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "de-indent a multiline string",
			input: `
      hello
      world
    `,
			want: "hello\nworld",
		},
		{
			name:  "handle strings with no leading or trailing newlines",
			input: "hello\nworld",
			want:  "hello\nworld",
		},
		{
			name: "handle strings with extra spaces around lines",
			input: `
        hello  
        world  
    `,
			want: "hello\nworld",
		},
		{
			name:  "handle empty strings",
			input: "",
			want:  "",
		},
		{
			name: "handle strings with only whitespace",
			input: `
      
    `,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stringutil.Multiline(tt.input); got != tt.want {
				t.Errorf("Multiline() = %v, want %v", got, tt.want)
			}
		})
	}
}
