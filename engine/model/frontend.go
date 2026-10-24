package model

import "encoding/json"

// FrontendFormatVersion is the format of frontend.json.
const FrontendFormatVersion = 1

// Frontend stage statuses.
const (
	FrontendChecked  = "checked"  // the pages were collected and compared
	FrontendSkipped  = "skipped"  // the change touches no frontend file
	FrontendWithheld = "withheld" // the stage could not finish; nothing is claimed
)

// Frontend is frontend.json: the outcome of the browser-free page check.
type Frontend struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	// Mode is "static" for a declared frontend build served by the engine, or
	// "server" for pages the backend renders itself.
	Mode          string     `json:"mode,omitempty"`
	Base          string     `json:"base,omitempty"`
	Head          string     `json:"head,omitempty"`
	ChangedFiles  []string   `json:"changed_files,omitempty"`
	Dropped       []string   `json:"dropped,omitempty"` // journey steps the validator refused, with reasons
	Journeys      int        `json:"journeys,omitempty"`
	NodeVersion   string     `json:"node_version,omitempty"`
	Compare       *FECompare `json:"compare,omitempty"`
	NotChecked    []string   `json:"not_checked"`
	StartedAt     string     `json:"started_at"`
	EndedAt       string     `json:"ended_at"`
	FailedStage   string     `json:"failed_stage,omitempty"`
	FailedMessage string     `json:"failed_message,omitempty"`
}

// FENotChecked is printed with every frontend result, whatever its status.
var FENotChecked = []string{"color-contrast", "visual layout", "real paint/CPU timing"}

// FECompare is fecheck's compare output.
type FECompare struct {
	Journeys     []FEJourney `json:"journeys"`
	Noise        *FENoise    `json:"noise"`
	BrokenOnBase []FEBroken  `json:"brokenOnBase"`
}

// FEJourney holds one journey's findings. Detail is kept as fecheck wrote it,
// because its shape depends on the metric.
type FEJourney struct {
	ID       string      `json:"id"`
	Findings []FEFinding `json:"findings"`
	RenderMs struct {
		Base int `json:"base"`
		Head int `json:"head"`
	} `json:"renderMs"`
}

// FEFinding is one metric that moved, with fecheck's verdict.
type FEFinding struct {
	Metric  string          `json:"metric"`
	Verdict string          `json:"verdict"` // regression, improved or withheld
	Detail  json.RawMessage `json:"detail"`
}

// FENoise is the base-against-base check.
type FENoise struct {
	Deterministic bool `json:"deterministic"`
	Unstable      []struct {
		ID string `json:"id"`
	} `json:"unstable"`
}

// FEBroken is a journey whose steps already fail on the base build.
type FEBroken struct {
	ID       string `json:"id"`
	Failures []struct {
		Error string `json:"error"`
	} `json:"failures"`
}
