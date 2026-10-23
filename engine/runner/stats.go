package runner

import (
	"errors"
	"fmt"
	"io/fs"

	"downstream/engine/model"
)

// procSample is everything runPass read from the kernel about the app.
type procSample struct {
	ticks0, ticks1 uint64
	rssMB          float64

	io0, io1 procIO
	ioErr    error

	threads    int
	threadsErr error
	fds        int
	fdsErr     error

	// exactCPU is user+system seconds from the exited process's rusage.
	exactCPU  float64
	haveExact bool
	completed int // journey iterations that finished every step
}

// buildProcess turns a sample into ProcessStats and the lines for
// Run.NotMeasured, one per counter that could not be read. A counter that
// could not be read stays 0 and is named in Unmeasured, never reported as 0.
func buildProcess(s procSample) (model.ProcessStats, []string) {
	ps := model.ProcessStats{
		CPUSeconds:        float64(s.ticks1-s.ticks0) / clockTicksPerSecond,
		PeakRSSMB:         s.rssMB,
		CompletedJourneys: s.completed,
	}
	var notes []string
	unmeasured := func(names ...string) { ps.Unmeasured = append(ps.Unmeasured, names...) }

	// The rusage figure covers the whole process lifetime (start, readiness
	// polling, the k6 window, shutdown). Every pass starts a fresh process the
	// same way, so that fixed part is the same in B1, C and B2 and cancels in
	// the comparison. Ticks cover only the k6 window but are quantised to 10 ms.
	if s.haveExact {
		ps.CPUSecondsExact, ps.CPUSource = s.exactCPU, model.CPUSourceRusage
	} else {
		ps.CPUSecondsExact, ps.CPUSource = ps.CPUSeconds, model.CPUSourceProcStat
	}

	ioErr := s.ioErr
	if ioErr == nil && (s.io1.WChar < s.io0.WChar || s.io1.SyscW < s.io0.SyscW) {
		ioErr = errors.New("a write counter went backwards")
	}
	if ioErr != nil {
		unmeasured(model.UnmeasuredWriteBytes, model.UnmeasuredWriteSyscalls)
		notes = append(notes, fmt.Sprintf("write bytes and write syscalls: not measured; /proc/<pid>/io was not readable (%s)", errText(ioErr)))
	} else {
		ps.WriteBytes = float64(s.io1.WChar - s.io0.WChar)
		ps.WriteSyscalls = float64(s.io1.SyscW - s.io0.SyscW)
	}
	if s.fdsErr != nil {
		unmeasured(model.UnmeasuredOpenFDs)
		notes = append(notes, fmt.Sprintf("open file descriptors: not measured; /proc/<pid>/fd was not readable (%s)", errText(s.fdsErr)))
	} else {
		ps.OpenFDs = s.fds
	}
	if s.threadsErr != nil {
		unmeasured(model.UnmeasuredThreads)
		notes = append(notes, fmt.Sprintf("thread count: not measured; the Threads line of /proc/<pid>/status was not readable (%s)", errText(s.threadsErr)))
	} else {
		ps.Threads = s.threads
	}

	if s.completed <= 0 {
		unmeasured(model.UnmeasuredCPUPerJourney, model.UnmeasuredWriteBytesPerJourney, model.UnmeasuredWriteSyscallsPerJourney)
		notes = append(notes, "per-journey figures: not measured; k6 reported no completed journey iteration in a pass")
		return ps, notes
	}
	n := float64(s.completed)
	ps.CPUMsPerJourney = ps.CPUSecondsExact * 1000 / n
	if ioErr == nil {
		ps.WriteBytesPerJourney = ps.WriteBytes / n
		ps.WriteSyscallsPerJourney = ps.WriteSyscalls / n
	} else {
		unmeasured(model.UnmeasuredWriteBytesPerJourney, model.UnmeasuredWriteSyscallsPerJourney)
	}
	return ps, notes
}

// errText drops the path from file errors so the same failure reads the same in
// every pass (the pid differs) and the note can be de-duplicated.
func errText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
