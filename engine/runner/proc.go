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

// procDir returns the /proc directory of pid, a child of this process. The Duo
// sandbox runs commands in their own pid namespace but leaves the outer /proc
// mounted, so the child's number there differs. There the child is found by
// its parent (this process, as /proc/self names it) and the last NSpid field.
func procDir(pid int) (string, error) {
	link, err := os.Readlink("/proc/self")
	if err != nil {
		return "", err
	}
	self, err := strconv.Atoi(link)
	if err != nil {
		return "", fmt.Errorf("unexpected /proc/self link %q", link)
	}
	if self == os.Getpid() {
		return fmt.Sprintf("/proc/%d", pid), nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			continue
		}
		if ppid, nspid, ok := parseStatusIDs(string(b)); ok && ppid == self && nspid == pid {
			return "/proc/" + e.Name(), nil
		}
	}
	return "", fmt.Errorf("no /proc entry for app pid %d; /proc is from another pid namespace, where this process is %d", pid, self)
}

// parseStatusIDs returns PPid and the innermost NSpid from /proc/<pid>/status.
func parseStatusIDs(status string) (ppid, nspid int, ok bool) {
	var havePPid, haveNSpid bool
	for _, line := range strings.Split(status, "\n") {
		if rest, found := strings.CutPrefix(line, "PPid:"); found {
			v, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return 0, 0, false
			}
			ppid, havePPid = v, true
		} else if rest, found := strings.CutPrefix(line, "NSpid:"); found {
			f := strings.Fields(rest)
			if len(f) == 0 {
				return 0, 0, false
			}
			v, err := strconv.Atoi(f[len(f)-1])
			if err != nil {
				return 0, 0, false
			}
			nspid, haveNSpid = v, true
		}
	}
	return ppid, nspid, havePPid && haveNSpid
}

func readCPUTicks(dir string) (uint64, error) {
	b, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return 0, err
	}
	return parseStatTicks(string(b))
}

func readPeakRSSMB(dir string) (float64, error) {
	b, err := os.ReadFile(dir + "/status")
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
