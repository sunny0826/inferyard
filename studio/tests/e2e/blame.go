package e2e

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

const (
	BlameCLI    = "cli"
	BlameStudio = "studio"
	BlameNone   = "none"
)

type BlameReport struct {
	Verdict     string
	Command     string
	ExitCode    int
	Limitations []string
	StderrTail  string
	StdoutFile  string
	StderrFile  string
}

func analyzeFailure(recorder *Recorder) BlameReport {
	records := recorder.Records()
	if len(records) == 0 {
		return BlameReport{Verdict: BlameNone}
	}
	last := records[len(records)-1]
	report := BlameReport{
		Command:    strings.Join(last.Argv, " "),
		ExitCode:   last.ExitCode,
		StdoutFile: last.StdoutFile,
		StderrFile: last.StderrFile,
	}
	if data, err := os.ReadFile(last.StderrFile); err == nil {
		report.StderrTail = tail(string(data), 400)
	}
	stdout, _ := os.ReadFile(last.StdoutFile)
	if last.ExitCode != 0 {
		report.Verdict = BlameCLI
		var parsed struct {
			Limitations []string `json:"limitations"`
		}
		if json.Unmarshal(stdout, &parsed) == nil {
			report.Limitations = parsed.Limitations
		}
		return report
	}
	var probe any
	if json.Unmarshal(stdout, &probe) != nil {
		report.Verdict = BlameCLI
		return report
	}
	report.Verdict = BlameStudio
	return report
}

func (b BlameReport) String() string {
	var sb strings.Builder
	sb.WriteString("blame=" + b.Verdict)
	if b.Command != "" {
		sb.WriteString(" command=" + b.Command)
	}
	sb.WriteString(" exit=" + strconv.Itoa(b.ExitCode))
	if len(b.Limitations) > 0 {
		sb.WriteString(" limitations=" + strings.Join(b.Limitations, ","))
	}
	if b.StderrTail != "" {
		sb.WriteString(" stderr=" + b.StderrTail)
	}
	return sb.String()
}

func tail(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return "…" + s[len(s)-limit:]
}
