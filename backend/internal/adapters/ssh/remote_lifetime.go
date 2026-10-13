package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
)

// RunWithLifetime gives an operational command its own remote process group.
// An open SSH stdin is its lifeline: cancellation, a lost transport, or a panel
// exit closes it. A node-local deadline also bounds a half-open transport.
// Cleanup signals only this command's process group and its owned watchdog.
func (s *RemoteSession) RunWithLifetime(ctx context.Context, command string, privileged bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	budget := 2 * time.Minute
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < budget {
		budget = time.Until(deadline)
	}
	bounded, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return s.runWithLifetime(bounded, command, privileged, budget)
}

func (s *RemoteSession) runWithLifetime(ctx context.Context, command string, privileged bool, budget time.Duration) (string, error) {
	if budget <= 0 {
		return "", context.DeadlineExceeded
	}
	if budget < time.Millisecond {
		budget = time.Millisecond // GNU timeout treats a rounded zero as unlimited.
	}
	if s == nil || s.client == nil || s.prepare == nil {
		return "", errors.New("SSH remote session is unavailable")
	}
	prepared, prefix, pty, err := s.prepare(remoteLifetimeCommand(command, budget), privileged)
	if err != nil {
		return "", err
	}
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	raw, err := s.client.newSession()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	defer raw.close()
	session, ok := raw.(interface {
		stdinPipe() (io.WriteCloser, error)
		setOutput(io.Writer, io.Writer)
		start(string) error
		wait() error
	})
	if !ok {
		return "", errors.New("SSH command lifetime streaming is unavailable")
	}
	if pty {
		if err := raw.requestPTY("xterm", 24, 80, cryptossh.TerminalModes{cryptossh.ECHO: 0}); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", err
		}
	}
	stdin, err := session.stdinPipe()
	if err != nil {
		return "", err
	}
	defer stdin.Close()
	stop()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var stdout, stderr bytes.Buffer
	session.setOutput(&stdout, &stderr)
	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		err := session.start(prepared)
		if err == nil {
			if prefix != "" {
				_, err = io.WriteString(stdin, prefix)
			}
			err = errors.Join(err, session.wait())
		}
		done <- result{strings.TrimSpace(stdout.String() + stderr.String()), err}
	}()
	select {
	case result := <-done:
		return result.output, result.err
	case <-ctx.Done():
		// EOF reaches the independent remote watchdog before closing SSH.
		_ = stdin.Close()
		select {
		case result := <-done:
			return result.output, ctx.Err()
		case <-time.After(8 * time.Second):
			_ = s.Close()
			return "", ctx.Err()
		}
	}
}

func remoteLifetimeCommand(command string, budget time.Duration) string {
	return fmt.Sprintf(`set -eu
command -v setsid >/dev/null
command -v timeout >/dev/null
test -x /bin/kill
exec 3<&0
setsid timeout --signal=TERM --kill-after=3s %.3fs sh -c %s < /dev/null 3<&- &
managed=$!
watcher=""
cleanup() {
  trap - EXIT HUP INT TERM
  /bin/kill -s TERM -- "-$managed" 2>/dev/null || true
  if /bin/kill -0 -- "-$managed" 2>/dev/null; then
    sleep 3
    /bin/kill -s KILL -- "-$managed" 2>/dev/null || true
  fi
  if [ -n "$watcher" ]; then
    kill -s TERM "$watcher" 2>/dev/null || true
    wait "$watcher" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 143' HUP INT TERM
(
  cat <&3 >/dev/null 3<&- &
  reader=$!
  trap 'kill -s TERM "$reader" 2>/dev/null || true; wait "$reader" 2>/dev/null || true; exit 0' HUP INT TERM
  wait "$reader" || true
  /bin/kill -s TERM -- "-$managed" 2>/dev/null || true
  sleep 3
  /bin/kill -s KILL -- "-$managed" 2>/dev/null || true
) <&3 &
watcher=$!
set +e
wait "$managed"
status=$?
exit "$status"
`, budget.Seconds(), shellQuote(command))
}
