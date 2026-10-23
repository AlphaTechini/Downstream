// Package model holds the JSON documents the engine reads and writes:
// scan.json, journeys.json, notes.json and run.json. Every package in the
// engine exchanges data through these types, so field names here are the
// file formats.
package model

import "encoding/json"

// FormatVersion is written into every document the engine produces.
const FormatVersion = 1

// Scan is scan.json: what a diff changed and which routes it can reach.
type Scan struct {
	Version      int           `json:"version"`
	Base         string        `json:"base"`
	Head         string        `json:"head"`
	Root         string        `json:"root"`   // directory analysed, relative to the repo
	Method       string        `json:"method"` // one line describing the analysis and its limits
	ChangedFiles []ChangedFile `json:"changed_files"`
	ChangedFuncs []string      `json:"changed_funcs"` // functions whose body or declarations they use overlap a hunk
	Routes       []Route       `json:"routes"`        // route table of the head build
	Changed      []Impact      `json:"changed"`       // routes whose handler call path reaches a changed function
	Affected     []Impact      `json:"affected"`      // routes that share a dependency with a changed route
	Notes        []string      `json:"notes,omitempty"`
}

// ChangedFile is one file in the diff with its hunks.
type ChangedFile struct {
	Path  string `json:"path"`
	Hunks []Hunk `json:"hunks"`
}

// Hunk is a line range on the old and new side of a unified diff.
type Hunk struct {
	OldStart int    `json:"old_start"`
	OldLines int    `json:"old_lines"`
	NewStart int    `json:"new_start"`
	NewLines int    `json:"new_lines"`
	Text     string `json:"text,omitempty"` // the hunk body, capped in size
}

// Route is one registered HTTP route.
type Route struct {
	Method     string   `json:"method"` // "" when the pattern has no method
	Path       string   `json:"path"`   // ServeMux pattern path, e.g. /books/{id}
	Handler    string   `json:"handler"`
	Middleware []string `json:"middleware,omitempty"` // outermost first
	File       string   `json:"file"`
	Line       int      `json:"line"`
}

// Impact is a route in the blast radius and why it is there.
type Impact struct {
	Route
	Reasons      []string `json:"reasons"`
	ChangedFuncs []string `json:"changed_funcs,omitempty"` // changed functions on its call path
}

// JourneyFile is journeys.json, written by the model and validated by the engine.
type JourneyFile struct {
	Version  int       `json:"version"`
	Journeys []Journey `json:"journeys"`
}

// Journey is what one user does, in order.
type Journey struct {
	ID          string `json:"id"` // [a-z0-9_]{1,40}
	Goal        string `json:"goal"`
	WhyIncluded string `json:"why_included"`
	Steps       []Step `json:"steps"`
}

// Step is one request. Strings in Path, Headers and Body may contain {{var}}
// placeholders filled from earlier Extract results or the built-in {{uniq}}.
type Step struct {
	Name         string            `json:"name"` // [a-z0-9_]{1,40}, unique within the journey
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         json.RawMessage   `json:"body,omitempty"`
	ExpectStatus int               `json:"expect_status"`
	Extract      map[string]string `json:"extract,omitempty"` // var name -> JSON pointer into the response body
}

// Notes is notes.json, written by the model: a likely cause and fix options
// per regression. The engine places the text; it never computes from it.
type Notes struct {
	Version int             `json:"version"`
	Items   map[string]Note `json:"items"` // keyed by Finding.ID
}

// Note is the model's text for one finding.
type Note struct {
	LikelyCause string   `json:"likely_cause"`
	FixOptions  []string `json:"fix_options"`
}

// Run is run.json: the three passes, discarded journeys and findings.
type Run struct {
	Version     int       `json:"version"`
	Base        string    `json:"base"`
	Head        string    `json:"head"`
	StartedAt   string    `json:"started_at"` // RFC 3339
	EndedAt     string    `json:"ended_at"`
	Settings    Settings  `json:"settings"`
	Journeys    []Journey `json:"journeys"` // the journeys that were kept
	Discarded   []Discard `json:"discarded"`
	Passes      []Pass    `json:"passes"` // in run order: B1, C, B2
	Findings    []Finding `json:"findings"`
	NotMeasured []string  `json:"not_measured"`
	Error       *RunError `json:"error,omitempty"` // set when the run stopped early
}

// Settings records how the run was configured.
type Settings struct {
	VUs             int        `json:"vus"`
	Iterations      int        `json:"iterations"` // journey iterations per VU in each pass
	DurationSeconds int        `json:"duration_seconds"`
	K6Version       string     `json:"k6_version"`
	Thresholds      Thresholds `json:"thresholds"`
}

// Thresholds are the classification rules in effect (see agentnotes.md).
type Thresholds struct {
	BandMultiplier    float64 `json:"band_multiplier"`     // regression needs delta > this * noise band
	MinRelative       float64 `json:"min_relative"`        // and delta >= this fraction of the worse baseline
	MinLatencyMS      float64 `json:"min_latency_ms"`      // and delta >= this for latency metrics
	MinMemoryMB       float64 `json:"min_memory_mb"`       // and delta >= this for memory
	MinCPUSeconds     float64 `json:"min_cpu_seconds"`     // and delta >= this for CPU time
	MaxBaselineSpread float64 `json:"max_baseline_spread"` // verdict withheld when |B1-B2| / min(B1,B2) exceeds this

	// Tolerances are the gated metrics' allowed changes, keyed by metric name.
	// The fields above judge the advisory latency metrics. A run.json written
	// before gated metrics existed has no Tolerances.
	Tolerances map[string]Tolerance `json:"tolerances"`
}

// Tolerance is how far a gated metric may rise above the worse baseline before
// a rise counts: the larger of Relative times the worse baseline and Absolute.
type Tolerance struct {
	Relative float64 `json:"relative"` // fraction of the worse baseline, 0.01 is 1%
	Absolute float64 `json:"absolute"` // in the metric's unit
}

// Discard is a journey dropped because it failed on the baseline.
type Discard struct {
	Journey string `json:"journey"`
	Step    string `json:"step"`
	Reason  string `json:"reason"`
}

// Pass is one start of one build with the journeys run against it.
type Pass struct {
	Label     string        `json:"label"` // B1, C or B2
	Commit    string        `json:"commit"`
	StartedAt string        `json:"started_at"`
	EndedAt   string        `json:"ended_at"`
	Process   ProcessStats  `json:"process"`
	Steps     []StepMetrics `json:"steps"`
}

// ProcessStats are read from /proc (and from the kernel's resource usage for
// the exited process) for the app process in one pass. Fields after PeakRSSMB
// were added later; run.json files without them still parse, and a zero
// CPUSource marks such a file.
type ProcessStats struct {
	CPUSeconds float64 `json:"cpu_seconds"` // user+system time consumed while k6 ran, in 10 ms ticks
	PeakRSSMB  float64 `json:"peak_rss_mb"` // VmHWM at the end of the pass

	// CPUSecondsExact is the figure CPU per journey is computed from. With
	// CPUSource "schedstat" it is CPUSecondsWindow; with "rusage" it is user+system
	// time over the whole process lifetime (start-up and shutdown included) at
	// microsecond resolution; with "proc-stat" it is the tick value above.
	CPUSecondsExact float64 `json:"cpu_seconds_exact"`
	CPUSource       string  `json:"cpu_source"` // "schedstat", "rusage" or "proc-stat"

	// CPUSecondsWindow is the time the app's threads spent on a CPU between just
	// before k6 started and just after it exited, at nanosecond resolution, from
	// /proc/<pid>/task/*/schedstat. It is 0 unless CPUSource is "schedstat".
	CPUSecondsWindow float64 `json:"cpu_seconds_window"`

	WriteBytes    float64 `json:"write_bytes"`    // wchar delta over the k6 run
	WriteSyscalls float64 `json:"write_syscalls"` // syscw delta over the k6 run
	OpenFDs       int     `json:"open_fds"`       // after k6 exited and idle connections drained
	Threads       int     `json:"threads"`        // same moment as OpenFDs

	CompletedJourneys       int     `json:"completed_journeys"` // iterations that finished every step without a failure
	CPUMsPerJourney         float64 `json:"cpu_ms_per_journey"`
	WriteBytesPerJourney    float64 `json:"write_bytes_per_journey"`
	WriteSyscallsPerJourney float64 `json:"write_syscalls_per_journey"`

	// Go runtime counters, read from the app over a loopback side port during
	// the pass. They exist only when the target config sets main_dir and the
	// build with the injected file succeeded; otherwise they are in Unmeasured.
	AllocObjectsPerJourney float64 `json:"alloc_objects_per_journey"` // heap objects allocated during the k6 window
	AllocBytesPerJourney   float64 `json:"alloc_bytes_per_journey"`   // heap bytes allocated during the k6 window
	GCCycles               float64 `json:"gc_cycles"`                 // completed GC cycles during the k6 window; gated
	GCCyclesPerJourney     float64 `json:"gc_cycles_per_journey"`     // GCCycles over completed journeys; for display only
	Goroutines             int     `json:"goroutines"`                // after k6 exited and idle connections drained

	// Unmeasured names each counter above that could not be read in this pass
	// (its value is then 0 and means nothing).
	Unmeasured []string `json:"unmeasured,omitempty"`
}

// CPUSource values.
const (
	CPUSourceSchedstat = "schedstat"
	CPUSourceRusage    = "rusage"
	CPUSourceProcStat  = "proc-stat"
)

// Names used in ProcessStats.Unmeasured.
const (
	UnmeasuredWriteBytes              = "write_bytes"
	UnmeasuredWriteSyscalls           = "write_syscalls"
	UnmeasuredOpenFDs                 = "open_fds"
	UnmeasuredThreads                 = "threads"
	UnmeasuredCPUPerJourney           = "cpu_ms_per_journey"
	UnmeasuredWriteBytesPerJourney    = "write_bytes_per_journey"
	UnmeasuredWriteSyscallsPerJourney = "write_syscalls_per_journey"
	UnmeasuredAllocObjectsPerJourney  = "alloc_objects_per_journey"
	UnmeasuredAllocBytesPerJourney    = "alloc_bytes_per_journey"
	UnmeasuredGCCycles                = "gc_cycles"
	UnmeasuredGCCyclesPerJourney      = "gc_cycles_per_journey"
	UnmeasuredGoroutines              = "goroutines"
)

// StepMetrics are k6 figures for one step in one pass. Latencies are in ms.
type StepMetrics struct {
	Journey  string  `json:"journey"`
	Step     string  `json:"step"`
	Count    int     `json:"count"`
	Failures int     `json:"failures"`
	P50      float64 `json:"p50_ms"`
	P95      float64 `json:"p95_ms"`
	Mean     float64 `json:"mean_ms"`
}

// Classes of finding.
const (
	ClassRegression = "regression"
	ClassTradeOff   = "trade-off"
	ClassImproved   = "improved"
	ClassNoise      = "noise"
	ClassWithheld   = "withheld" // baseline runs disagreed too much to judge
	// ClassNotReached marks a latency figure for a step that recorded no
	// requests in at least one pass, so there is nothing to compare.
	ClassNotReached = "not-reached"
	// ClassNotMeasured marks a gated counter that could not be read in at least
	// one pass, so there is nothing to compare.
	ClassNotMeasured = "not-measured"
)

// Kinds of metric. Counter, CPU, memory and failures are gated: they decide
// whether a run has a regression. Latency is advisory.
const (
	KindCounter  = "counter"
	KindCPU      = "cpu"
	KindMemory   = "memory"
	KindFailures = "failures"
	KindLatency  = "latency"
)

// Metric names used in findings.
const (
	MetricP95      = "p95_ms"
	MetricP50      = "p50_ms"
	MetricMean     = "mean_ms"
	MetricFailures = "failures"
	MetricCPU      = "cpu_seconds"
	MetricRSS      = "peak_rss_mb"

	MetricCPUPerJourney           = "cpu_ms_per_journey"
	MetricWriteBytesPerJourney    = "write_bytes_per_journey"
	MetricWriteSyscallsPerJourney = "write_syscalls_per_journey"
	MetricOpenFDs                 = "open_fds"
	MetricThreads                 = "threads"
	MetricAllocObjectsPerJourney  = "alloc_objects_per_journey"
	MetricAllocBytesPerJourney    = "alloc_bytes_per_journey"
	MetricGCCycles                = "gc_cycles"
	MetricGCCyclesPerJourney      = "gc_cycles_per_journey" // run.json files from before gc_cycles; never gated now
	MetricGoroutines              = "goroutines"
)

// Finding compares one metric across B1, C and B2.
type Finding struct {
	ID       string  `json:"id"`      // stable: <journey>.<step>.<metric>, or app.<metric>
	Journey  string  `json:"journey"` // "" for app-wide process metrics
	Step     string  `json:"step"`
	Metric   string  `json:"metric"`
	Unit     string  `json:"unit"` // ms, s, MB, count
	B1       float64 `json:"b1"`
	C        float64 `json:"c"`
	B2       float64 `json:"b2"`
	Band     float64 `json:"band"`      // |B1 - B2|
	Delta    float64 `json:"delta"`     // C minus the worse baseline (positive is worse)
	DeltaPct float64 `json:"delta_pct"` // Delta as a percentage of the worse baseline
	Class    string  `json:"class"`
	Reason   string  `json:"reason"` // which rule decided the class, in words

	// Kind says what sort of metric this is (the Kind constants). Gated findings
	// can open issues; the others are advisory. A run.json written before gating
	// existed has neither field: see Decides.
	Kind  string `json:"kind"`
	Gated bool   `json:"gated"`

	// Allowed is the tolerance in effect for a gated metric (zero for the rest).
	// Detectable is the smallest change this run could have told apart from
	// noise: the larger of Allowed and Band for a gated metric. DetectablePct
	// is Detectable as a percentage of the worse baseline.
	Allowed       float64 `json:"allowed"`
	Detectable    float64 `json:"detectable"`
	DetectablePct float64 `json:"detectable_pct"`
}

// Decides reports whether the finding may count toward a verdict or an issue.
// A finding from a run.json that predates Kind is treated as deciding, as it
// did then.
func (f Finding) Decides() bool { return f.Gated || f.Kind == "" }

// RunError says where a run stopped and why.
type RunError struct {
	Stage   string `json:"stage"` // e.g. "build (head)", "validate", "pass C"
	Message string `json:"message"`
}

// Marker is the JSON inside the hidden <!-- downstream:v1 ... --> comment of an issue.
type Marker struct {
	Version    int      `json:"v"`
	MR         int      `json:"mr"`
	Base       string   `json:"base"`
	Head       string   `json:"head"`
	FindingID  string   `json:"finding"`
	Metric     string   `json:"metric"`
	Step       string   `json:"step"`
	Journey    Journey  `json:"journey"`
	B1         float64  `json:"b1"`
	C          float64  `json:"c"`
	B2         float64  `json:"b2"`
	Band       float64  `json:"band"`
	FixOptions []string `json:"fix_options"`
}
