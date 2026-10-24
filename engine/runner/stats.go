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

	// sched0 and sched1 are the summed on-CPU nanoseconds of the app's threads
	// just before k6 started and just after it exited. schedOK says both were
	// read; schedErr is why they were not.
	sched0, sched1 uint64
	schedOK        bool
	schedErr       error

	// exactCPU is user+system seconds from the exited process's rusage.
	exactCPU  float64
	haveExact bool
	completed int      // journey iterations that finished every step
	received  *float64 // bytes k6 received from the app; nil when k6 did not report them

	// rt0 and rt1 bracket the k6 window; rtDrained is read after the drain.
	// rtErr is why they are unusable; with rtQuiet set the reason is already
	// in Run.NotMeasured from the build stage, so no line is added per pass.
	rt0, rt1, rtDrained runtimeCounters
	rtErr               error
	rtQuiet             bool
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
	unmeasured := func(names ...string) { ps.Unmeasured = append(ps.Unmeasured, names...) }

	// Preferred: schedstat over the k6 window, in nanoseconds. The rusage figure
	// covers the whole process lifetime (start, readiness polling, the k6
	// window, shutdown), so a head that adds heavy initialisation would show as
	// a per-journey rise; it is the fallback. Ticks cover only the k6 window but
	// are quantised to 10 ms.
	schedErr := s.schedErr
	if schedErr == nil && s.schedOK && s.sched1 < s.sched0 {
		schedErr = errors.New("the summed on-CPU time went backwards, so a thread exited inside the window")
	}
	switch {
	case schedErr == nil && s.schedOK:
		ps.CPUSecondsWindow = float64(s.sched1-s.sched0) / 1e9
		ps.CPUSecondsExact, ps.CPUSource = ps.CPUSecondsWindow, model.CPUSourceSchedstat
	case s.haveExact:
		ps.CPUSecondsExact, ps.CPUSource = s.exactCPU, model.CPUSourceRusage
	default:
		ps.CPUSecondsExact, ps.CPUSource = ps.CPUSeconds, model.CPUSourceProcStat
	}
	var notes []string
	if schedErr != nil {
		notes = append(notes, fmt.Sprintf("CPU over the k6 window: not measured from schedstat (%s); CPU per journey uses %s", errText(schedErr), ps.CPUSource))
	}

	ioErr := s.ioErr
	if ioErr == nil && (s.io1.WChar < s.io0.WChar || s.io1.SyscW < s.io0.SyscW) {
		ioErr = errors.New("a write counter went backwards")
	}
	if ioErr != nil {
		unmeasured(model.UnmeasuredWriteBytes, model.UnmeasuredWriteSyscalls)
		notes = append(notes, fmt.Sprintf("write bytes and write syscalls: not measured; /proc/<pid>/io was not readable (%s)", errText(ioErr)))
	} else {
		ps.WriteSyscalls = float64(s.io1.SyscW - s.io0.SyscW)
		// wchar also counts the responses the app sends, so a change that makes a
		// response bigger would look like extra writing. The bytes k6 received are
		// those responses; what is left is logs, files and other output.
		if s.received == nil {
			unmeasured(model.UnmeasuredWriteBytes)
			notes = append(notes, "write bytes: not measured; k6 did not report the bytes it received, so responses cannot be told apart from other writes")
		} else {
			ps.ResponseBytes = *s.received
			ps.WriteBytes = float64(s.io1.WChar-s.io0.WChar) - *s.received
			if ps.WriteBytes < 0 {
				ps.WriteBytes = 0
			}
		}
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

	var allocObjects, allocBytes, gcCycles uint64
	rtErr := s.rtErr
	if rtErr == nil {
		var e1, e2, e3 error
		allocObjects, e1 = counterDelta("allocated objects", s.rt0.AllocObjects, s.rt1.AllocObjects)
		allocBytes, e2 = counterDelta("allocated bytes", s.rt0.AllocBytes, s.rt1.AllocBytes)
		gcCycles, e3 = counterDelta("GC cycles", s.rt0.GCCycles, s.rt1.GCCycles)
		for _, e := range []error{e1, e2, e3} {
			if e != nil {
				rtErr = e
				break
			}
		}
	}
	if rtErr != nil {
		unmeasured(model.UnmeasuredAllocObjectsPerJourney, model.UnmeasuredAllocBytesPerJourney,
			model.UnmeasuredGCCycles, model.UnmeasuredGCCyclesPerJourney, model.UnmeasuredGoroutines)
		if !s.rtQuiet {
			notes = append(notes, fmt.Sprintf("runtime counters: not measured; %s", errText(rtErr)))
		}
	} else {
		ps.Goroutines = int(s.rtDrained.Goroutines)
		ps.GCCycles = float64(gcCycles)
	}

	if s.completed <= 0 {
		unmeasured(model.UnmeasuredCPUPerJourney, model.UnmeasuredWriteBytesPerJourney, model.UnmeasuredWriteSyscallsPerJourney)
		if rtErr == nil {
			unmeasured(model.UnmeasuredAllocObjectsPerJourney, model.UnmeasuredAllocBytesPerJourney, model.UnmeasuredGCCyclesPerJourney)
		}
		notes = append(notes, "per-journey figures: not measured; k6 reported no completed journey iteration in a pass")
		return ps, notes
	}
	n := float64(s.completed)
	if rtErr == nil {
		ps.AllocObjectsPerJourney = float64(allocObjects) / n
		ps.AllocBytesPerJourney = float64(allocBytes) / n
		ps.GCCyclesPerJourney = float64(gcCycles) / n
	}
	ps.CPUMsPerJourney = ps.CPUSecondsExact * 1000 / n
	if ioErr == nil {
		if s.received == nil {
			unmeasured(model.UnmeasuredWriteBytesPerJourney)
		} else {
			ps.WriteBytesPerJourney = ps.WriteBytes / n
			ps.ResponseBytesPerJourney = ps.ResponseBytes / n
		}
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
