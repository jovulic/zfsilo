package literal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jovulic/zfsilo/app/internal/command/literal"
	"github.com/jovulic/zfsilo/lib/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExecutor struct {
	execFunc func(ctx context.Context, cmd string) (*command.CommandResult, error)
}

func (m *mockExecutor) Exec(ctx context.Context, cmd string) (*command.CommandResult, error) {
	return m.execFunc(ctx, cmd)
}

func TestLiteral_Run(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		executor := &mockExecutor{
			execFunc: func(ctx context.Context, cmd string) (*command.CommandResult, error) {
				return &command.CommandResult{
					Stdout:   "  hello world  \n",
					ExitCode: 0,
				},
				nil
			},
		}

		l := literal.With(executor)
		stdout, err := l.Run(ctx, "echo 'hello world'")

		require.NoError(t, err)
		assert.Equal(t, "hello world", stdout)
	})

	t.Run("failure", func(t *testing.T) {
		executor := &mockExecutor{
			execFunc: func(ctx context.Context, cmd string) (*command.CommandResult, error) {
				return &command.CommandResult{
					Stderr:   "error message",
					ExitCode: 1,
				},
				errors.New("exit status 1")
			},
		}

		l := literal.With(executor)
		stdout, err := l.Run(ctx, "invalid-command")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "stderr: error message")
		assert.Empty(t, stdout)
	})
}

func TestLiteral_RunLines(t *testing.T) {
	ctx := context.Background()

	t.Run("success multiple lines", func(t *testing.T) {
		executor := &mockExecutor{
			execFunc: func(ctx context.Context, cmd string) (*command.CommandResult, error) {
				return &command.CommandResult{
					Stdout:   "  line1  \n  line2  \nline3",
					ExitCode: 0,
				},
				nil
			},
		}

		l := literal.With(executor)
		lines, err := l.RunLines(ctx, "cmd")

		require.NoError(t, err)
		assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
	})

	t.Run("empty output", func(t *testing.T) {
		executor := &mockExecutor{
			execFunc: func(ctx context.Context, cmd string) (*command.CommandResult, error) {
				return &command.CommandResult{
					Stdout:   "   \n  ",
					ExitCode: 0,
				},
				nil
			},
		}

		l := literal.With(executor)
		lines, err := l.RunLines(ctx, "cmd")

		require.NoError(t, err)
		assert.Nil(t, lines)
	})
}

func TestLiteral_RunResult(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		expectedResult := &command.CommandResult{
			Stdout:   "output",
			Stderr:   "err",
			ExitCode: 0,
		}
		executor := &mockExecutor{
			execFunc: func(ctx context.Context, cmd string) (*command.CommandResult, error) {
				return expectedResult, nil
			},
		}

		l := literal.With(executor)
		result, err := l.RunResult(ctx, "cmd")

		require.NoError(t, err)
		assert.Equal(t, expectedResult, result)
	})

	t.Run("failure returns result", func(t *testing.T) {
		expectedResult := &command.CommandResult{
			Stdout:   "output",
			Stderr:   "err",
			ExitCode: 1,
		}
		executor := &mockExecutor{
			execFunc: func(ctx context.Context, cmd string) (*command.CommandResult, error) {
				return expectedResult, errors.New("fail")
			},
		}

		l := literal.With(executor)
		result, err := l.RunResult(ctx, "cmd")

		require.Error(t, err)
		assert.Equal(t, expectedResult, result)
	})
}
