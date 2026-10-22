package runner

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// clockTicksPerSecond is the kernel's USER_HZ, the unit of utime and stime in
// /proc/<pid>/stat. It is 100 on x86_64 and arm64 Linux, the platforms the
// engine runs on, so no cgo call to sysconf is needed.
const clockTicksPerSecond = 100

// parseStatTicks returns utime+stime (fields 14 and 15) from the contents of
// /proc/<pid>/stat. The command name in field 2 may contain spaces and
// parentheses, so parsing starts after the last ')'.
func parseStatTicks(stat string) (uint64, error) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, fmt.Errorf("no command name in stat %q", truncate(stat, 60))
	}
	f := strings.Fields(stat[i+1:])
	// f[0] is field 3 (state), so field 14 is f[11] and field 15 is f[12].
	if len(f) < 13 {
		return 0, fmt.Errorf("stat has %d fields after the command, need 13", len(f))
	}
	u, err := strconv.ParseUint(f[11], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("utime: %w", err)
	}
	s, err := strconv.ParseUint(f[12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("stime: %w", err)
	}
	return u + s, nil
}

// parseVmHWMMB returns the peak resident set size in MB from the contents of
// /proc/<pid>/status (the VmHWM line, in kB).
func parseVmHWMMB(status string) (float64, error) {
	for _, line := range strings.Split(status, "\n") {
		rest, ok := strings.CutPrefix(line, "VmHWM:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 2 || f[1] != "kB" {
			return 0, fmt.Errorf("unexpected VmHWM line %q", line)
		}
		kb, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("VmHWM: %w", err)
		}
		return float64(kb) / 1024, nil
	}
	return 0, fmt.Errorf("no VmHWM line in status")
}

func readCPUTicks(pid int) (uint64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	return parseStatTicks(string(b))
}

func readPeakRSSMB(pid int) (float64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	return parseVmHWMMB(string(b))
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	if len(p) >= t.max {
		t.buf.Reset()
		p = p[len(p)-t.max:]
	} else if over := t.buf.Len() + len(p) - t.max; over > 0 {
		rest := append([]byte(nil), t.buf.Bytes()[over:]...)
		t.buf.Reset()
		t.buf.Write(rest)
	}
	t.buf.Write(p)
	return n, nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
