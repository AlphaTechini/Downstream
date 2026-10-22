package main

import (
	"flag"
	"strings"
	"testing"
)

func TestCommitsRejectsNonSHA(t *testing.T) {
	for _, bad := range []string{"", "HEAD", "main", "-x", "abc", "0123456789abcdef0123456789abcdef012345678", "g123456"} {
		err := commits(t.TempDir(), bad)
		if err == nil || !strings.Contains(err.Error(), "not a commit SHA") {
			t.Errorf("commits(%q) = %v, want a SHA error", bad, err)
		}
	}
}

func TestParseRequiresFlags(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.String("run", "", "")
	fs.String("scan", "", "")
	if err := parse(fs, []string{"--run", "r.json"}, "run", "scan"); err == nil || !strings.Contains(err.Error(), "--scan is required") {
		t.Errorf("missing --scan: got %v", err)
	}
	fs = flag.NewFlagSet("x", flag.ContinueOnError)
	fs.String("run", "", "")
	if err := parse(fs, []string{"--run", "r.json", "extra"}, "run"); err == nil {
		t.Error("stray argument accepted")
	}
}
