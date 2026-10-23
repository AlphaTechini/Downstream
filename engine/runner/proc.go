package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

// parseSchedstat returns the first field of /proc/<pid>/task/<tid>/schedstat:
// the nanoseconds the thread has spent on a CPU. The other two fields (time
// waiting on a run queue, number of time slices) are not used.
func parseSchedstat(s string) (uint64, error) {
	f := strings.Fields(s)
	if len(f) != 3 {
		return 0, fmt.Errorf("schedstat has %d fields, need 3", len(f))
	}
	ns, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("schedstat on-cpu time: %w", err)
	}
	return ns, nil
}

// sumSchedstat adds the on-CPU nanoseconds of every thread under taskDir
// (/proc/<pid>/task). A thread that exits between listing the directory and
// reading its file is skipped, and a thread that exits between two sums takes
// its time with it, so a later sum can be smaller than an earlier one; the
// caller treats that as unusable. Go runtime threads rarely exit, so on the
// app under test the sums are normally monotonic. An error is returned when the
// directory cannot be read or no thread file could (no schedstat support).
func sumSchedstat(taskDir string) (uint64, error) {
	ents, err := os.ReadDir(taskDir)
	if err != nil {
		return 0, err
	}
	var total uint64
	read := 0
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(taskDir, e.Name(), "schedstat"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		ns, err := parseSchedstat(string(b))
		if err != nil {
			return 0, fmt.Errorf("thread %s: %w", e.Name(), err)
		}
		total += ns
		read++
	}
	if read == 0 {
		return 0, errors.New("no thread has a readable schedstat file")
	}
	return total, nil
}

func readSchedstat(pid int) (uint64, error) {
	return sumSchedstat(fmt.Sprintf("/proc/%d/task", pid))
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

// procIO is /proc/<pid>/io. Only the write counters are reported: the read
// counters also count page-cache hits and vary with what else ran, so they are
// parsed but not used.
type procIO struct {
	RChar, WChar, SyscR, SyscW uint64
}

// parseProcIO reads the contents of /proc/<pid>/io. wchar and syscw must be
// present; rchar and syscr are optional.
func parseProcIO(s string) (procIO, error) {
	var io procIO
	seen := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		var dst *uint64
		switch name {
		case "rchar":
			dst = &io.RChar
		case "wchar":
			dst = &io.WChar
		case "syscr":
			dst = &io.SyscR
		case "syscw":
			dst = &io.SyscW
		default:
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 64)
		if err != nil {
			return procIO{}, fmt.Errorf("%s: %w", name, err)
		}
		*dst = v
		seen[name] = true
	}
	for _, need := range []string{"wchar", "syscw"} {
		if !seen[need] {
			return procIO{}, fmt.Errorf("no %s line in io", need)
		}
	}
	return io, nil
}

// parseThreads returns the Threads count from the contents of /proc/<pid>/status.
func parseThreads(status string) (int, error) {
	for _, line := range strings.Split(status, "\n") {
		rest, ok := strings.CutPrefix(line, "Threads:")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return 0, fmt.Errorf("Threads: %w", err)
		}
		if n < 1 {
			return 0, fmt.Errorf("Threads: implausible count %d", n)
		}
		return n, nil
	}
	return 0, fmt.Errorf("no Threads line in status")
}

func readProcIO(pid int) (procIO, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
	if err != nil {
		return procIO{}, err
	}
	return parseProcIO(string(b))
}

func readThreads(pid int) (int, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	return parseThreads(string(b))
}

// countFDs counts the entries of /proc/<pid>/fd.
func countFDs(pid int) (int, error) {
	ents, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return 0, err
	}
	return len(ents), nil
}

// procReader is where the runner gets the /proc counters. Tests replace
// procSource to simulate a counter that cannot be read.
type procReader struct {
	io      func(pid int) (procIO, error)
	threads func(pid int) (int, error)
	fds     func(pid int) (int, error)
}

var procSource = procReader{io: readProcIO, threads: readThreads, fds: countFDs}

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

// rusageSeconds returns user+system CPU time of an exited process at the
// resolution the kernel reports (microseconds). ok is false when the OS gave no
// rusage.
func rusageSeconds(ps *os.ProcessState) (secs float64, ok bool) {
	if ps == nil {
		return 0, false
	}
	ru, isRusage := ps.SysUsage().(*syscall.Rusage)
	if !isRusage || ru == nil {
		return 0, false
	}
	tv := func(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }
	return tv(ru.Utime) + tv(ru.Stime), true
}
