package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSchedstat(t *testing.T) {
	got, err := parseSchedstat("123456789 987 42\n")
	if err != nil || got != 123456789 {
		t.Errorf("got %d, %v", got, err)
	}
	for name, in := range map[string]string{
		"empty":         "",
		"two fields":    "1 2\n",
		"four fields":   "1 2 3 4\n",
		"not a number":  "abc 2 3\n",
		"negative":      "-1 2 3\n",
		"fraction":      "1.5 2 3\n",
		"overflow u64":  "18446744073709551616 2 3\n",
		"words instead": "run_delay 2 3\n",
	} {
		if _, err := parseSchedstat(in); err == nil {
			t.Errorf("%s: %q accepted", name, in)
		}
	}
}

// fakeTasks writes a fake /proc/<pid>/task tree: one directory per thread id.
func fakeTasks(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for tid, body := range files {
		if err := os.Mkdir(filepath.Join(dir, tid), 0o755); err != nil {
			t.Fatal(err)
		}
		if body == "" {
			continue // a thread directory without the file: it exited, or no schedstat support
		}
		if err := os.WriteFile(filepath.Join(dir, tid, "schedstat"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSumSchedstat(t *testing.T) {
	dir := fakeTasks(t, map[string]string{
		"100": "1000000000 5 3\n",
		"101": "250000000 0 1\n",
		"102": "1 0 1\n",
		"103": "", // vanished between the listing and the read
	})
	got, err := sumSchedstat(dir)
	if err != nil || got != 1250000001 {
		t.Errorf("sum = %d, %v; want 1250000001", got, err)
	}

	if _, err := sumSchedstat(fakeTasks(t, map[string]string{"1": "", "2": ""})); err == nil || !strings.Contains(err.Error(), "no thread") {
		t.Errorf("no readable file: %v", err)
	}
	if _, err := sumSchedstat(fakeTasks(t, map[string]string{"1": "1 2 3\n", "2": "garbage\n"})); err == nil || !strings.Contains(err.Error(), "thread 2") {
		t.Errorf("malformed file: %v", err)
	}
	if _, err := sumSchedstat(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing directory accepted")
	}
}

// On a kernel that offers schedstat the reading of this process is positive and
// grows while it burns CPU. Where the files do not exist the test is skipped:
// the runner then falls back to rusage, which other tests cover.
func TestReadSchedstatOfThisProcess(t *testing.T) {
	a, err := readSchedstat(os.Getpid())
	if err != nil {
		t.Skipf("schedstat not readable here: %v", err)
	}
	if a == 0 {
		t.Error("a running process shows no on-CPU time")
	}
	end := time.Now().Add(30 * time.Millisecond)
	x := 0
	for time.Now().Before(end) {
		x++
	}
	_ = x
	b, err := readSchedstat(os.Getpid())
	if err != nil || b <= a {
		t.Errorf("after spinning: %d then %d, %v", a, b, err)
	}
}
