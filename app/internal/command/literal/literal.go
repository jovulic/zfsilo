// Package literal contains lib/command wrappers for executing literal commands.
package literal

import (
	"context"
	"fmt"
	"strings"

	"github.com/jovulic/zfsilo/lib/command"
)

// Literal provides an interface for running arbitrary commands.
type Literal struct {
	executor command.Executor
}

// With creates a new Literal instance.
func With(executor command.Executor) Literal {
	return Literal{
		executor: executor,
	}
}

// Run executes a command and returns the trimmed stdout.
func (l Literal) Run(ctx context.Context, cmd string) (string, error) {
	result, err := l.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return "", fmt.Errorf("failed to run command '%s': %w, stderr: %s", cmd, err, stderr)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// RunLines executes a command and returns the stdout split into lines, with
// each line trimmed.
func (l Literal) RunLines(ctx context.Context, cmd string) ([]string, error) {
	stdout, err := l.Run(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if stdout == "" {
		return nil, nil
	}

	lines := strings.Split(stdout, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines, nil
}

// RunResult executes a command and returns the full command result.
func (l Literal) RunResult(ctx context.Context, cmd string) (*command.CommandResult, error) {
	result, err := l.executor.Exec(ctx, cmd)
	if err != nil {
		return result, fmt.Errorf("failed to run command '%s': %w", cmd, err)
	}
	return result, nil
}
