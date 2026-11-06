package engine

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// processSpec is one run of an AI CLI, whichever it is: the machinery that starts it, feeds it, stops it and reads
// its output is the same for every provider, only the arguments and the way the output is read differ.
type processSpec struct {
	// Name is the display name of the provider, for the messages the user reads.
	Name string
	// Shell is a login shell, so the CLI resolves like in a terminal even when the app starts from the Dock.
	Shell string
	// Script is run by `Shell -lc`; it must end with `exec "$0" "$@"`, which runs Command with Args as data.
	Script  string
	Command string
	Args    []string
	Dir     string
	Env     []string
	// Stdin carries the prompt: untrusted text never reaches the process list.
	Stdin     string
	Timeout   time.Duration
	KillGrace time.Duration
}

// processEnd is how a run ended, before the provider decides what it means.
type processEnd struct {
	cancelled bool
	timedOut  bool
	waitErr   error
	stderr    string
	// aborted is set when the line reader stopped the run on purpose.
	aborted error
}

// runProcess starts the CLI in its own process group and hands every line of its output to onLine. A non-nil error
// from onLine stops the whole group at once: it is how a provider gives up on a run that would only retry for minutes.
// Cancelling ctx or reaching the timeout stops the group too.
func runProcess(ctx context.Context, spec processSpec, onLine func(line string) error) (processEnd, error) {
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, spec.Timeout)
	defer cancelTimeout()

	args := append([]string{"-lc", spec.Script, spec.Command}, spec.Args...)
	cmd := exec.Command(spec.Shell, args...)
	cmd.Dir = spec.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = spec.Env
	cmd.Stdin = strings.NewReader(spec.Stdin)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return processEnd{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{buffer: &stderr, max: 2000}
	if err := cmd.Start(); err != nil {
		return processEnd{}, i18n.Wrap(err, "%s could not be started: %s", spec.Name, err)
	}

	abort := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		select {
		case <-timeoutCtx.Done():
			terminate(cmd, spec.KillGrace)
		case <-abort:
			terminate(cmd, spec.KillGrace)
		case <-finished:
		}
	}()

	var aborted error
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if aborted != nil {
			continue
		}
		if err := onLine(scanner.Text()); err != nil {
			aborted = err
			close(abort)
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	close(finished)

	return processEnd{
		cancelled: ctx.Err() != nil,
		timedOut:  timeoutCtx.Err() != nil,
		waitErr:   waitErr,
		stderr:    strings.TrimSpace(stderr.String()),
		aborted:   aborted,
	}, nil
}

// settle turns how a run ended into the error the engine understands: stopped by the user, timed out, failed in a way
// the provider reported (failure), or exited badly. nil means the run went through.
func (end processEnd) settle(spec processSpec, failure error) error {
	switch {
	case end.cancelled:
		return ErrCancelled
	case end.aborted != nil:
		return end.aborted
	case end.timedOut:
		return i18n.NewError("%s took more than %d min and was stopped", spec.Name, int(spec.Timeout.Minutes()))
	case failure != nil:
		return failure
	case end.waitErr != nil:
		detail := end.stderr
		if detail != "" {
			detail = ": " + shortLine(detail)
		}
		classified := ClassifyFailure(end.waitErr.Error() + detail)
		if errors.Is(classified, ErrLimit) || errors.Is(classified, ErrLogin) {
			return classified
		}
		return i18n.Wrap(end.waitErr, "%s exited with %s%s", spec.Name, end.waitErr, detail)
	}
	return nil
}

// terminate stops the whole process group, then forces it if the CLI does not leave.
func terminate(cmd *exec.Cmd, grace time.Duration) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(grace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
}

// limitedWriter keeps only the last max bytes written.
type limitedWriter struct {
	buffer *bytes.Buffer
	max    int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.buffer.Write(p)
	if over := w.buffer.Len() - w.max; over > 0 {
		w.buffer.Next(over)
	}
	return len(p), nil
}
