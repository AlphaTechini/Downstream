package runner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"downstream/engine/target"
)

const (
	readyTimeout = 30 * time.Second
	stopGrace    = 5 * time.Second
)

// pickPort asks the kernel for a free loopback port. The port is free when
// this returns; the small window before the app binds it is accepted.
func pickPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// app is one running instance of the target.
type app struct {
	cmd  *exec.Cmd
	pid  int
	port int
	out  *tailBuffer
	done chan struct{} // closed once the process has been reaped
}

// startApp launches the start command with exec so the shell is replaced by
// the app and the pid we sample in /proc is the app's own.
func startApp(cfg target.Config, bin, tree string, port int) (*app, error) {
	cmd := exec.Command("sh", "-c", "exec "+cfg.StartCommand(bin))
	cmd.Dir = tree
	cmd.Env = append(os.Environ(), cfg.PortEnv+"="+strconv.Itoa(port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out := &tailBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	a := &app{cmd: cmd, pid: cmd.Process.Pid, port: port, out: out, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(a.done)
	}()
	return a, nil
}

func (a *app) baseURL() string { return "http://127.0.0.1:" + strconv.Itoa(a.port) }

func (a *app) exited() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// waitReady polls readyPath until it answers 200. A redirect is not followed
// and counts as not ready, so the poll cannot leave loopback.
func (a *app) waitReady(ctx context.Context, readyPath string) error {
	client := &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	deadline := time.NewTimer(readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	url := a.baseURL() + readyPath
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.done:
			return fmt.Errorf("the app exited before it was ready; output tail:\n%s", a.out.String())
		case <-deadline.C:
			return fmt.Errorf("the app did not answer 200 on %s within %s", readyPath, readyTimeout)
		case <-tick.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

// stop ends the app's process group: SIGTERM, five seconds of grace, SIGKILL.
// It is safe to call more than once.
func (a *app) stop() {
	if !a.exited() {
		_ = syscall.Kill(-a.pid, syscall.SIGTERM)
		select {
		case <-a.done:
		case <-time.After(stopGrace):
		}
	}
	// Anything still in the group (children, or the app after the grace) goes now.
	_ = syscall.Kill(-a.pid, syscall.SIGKILL)
	select {
	case <-a.done:
	case <-time.After(stopGrace):
	}
}

// errAppDied is returned when the app is gone while it should be serving.
var errAppDied = errors.New("the app exited during the pass")
