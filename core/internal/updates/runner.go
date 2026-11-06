package updates

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Command is one git invocation. Args are passed to git as they are: there is never a shell.
type Command struct {
	// Dir is the working directory (the source checkout).
	Dir string
	// Env holds the variables the checker requires; a runner adds the rest of the environment it needs.
	Env  []string
	Args []string
}

// Runner runs a git command and returns its standard output. A failing command returns a *CommandError.
type Runner func(ctx context.Context, cmd Command) (string, error)

// CommandError is a git command that did not succeed. Stderr is only used to tell an offline Mac from a refused
// credential; it is never shown.
type CommandError struct {
	ExitCode int
	Stderr   string
}

func (e *CommandError) Error() string { return "git exited with status " + strconv.Itoa(e.ExitCode) }

const maxOutput = 1 << 20

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxOutput - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// ExecRunner runs the real git. It starts it directly (no shell), without a terminal on standard input, in the
// command's directory, with the inherited environment minus every GIT_* and POUPLE_* variable (they could redirect
// git to another repository or leak the API token) plus the command's own variables and LC_ALL=C.
func ExecRunner(ctx context.Context, cmd Command) (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		path = "/usr/bin/git"
	}
	process := exec.CommandContext(ctx, path, cmd.Args...)
	process.Dir = cmd.Dir
	process.Env = append(cleanEnvironment(os.Environ()), cmd.Env...)
	process.Env = append(process.Env, "LC_ALL=C")
	process.WaitDelay = 2 * time.Second
	var stdout, stderr cappedBuffer
	process.Stdout, process.Stderr = &stdout, &stderr
	if err := process.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && ctx.Err() == nil {
			return "", &CommandError{ExitCode: exit.ExitCode(), Stderr: stderr.String()}
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	return stdout.String(), nil
}

func cleanEnvironment(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		if strings.HasPrefix(entry, "GIT_") || strings.HasPrefix(entry, "POUPLE_") || strings.HasPrefix(entry, "LC_ALL=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
