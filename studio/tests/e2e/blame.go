package e2e

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// Blame verdicts for a failed chain step. The boundary rule is strict: the
// CLI owns everything up to and including its stdout bytes; Studio owns what
// it does with a well-formed answer.
const (
	BlameCLI    = "cli"    // non-zero exit, or stdout Studio cannot parse
	BlameStudio = "studio" // boundary honored; failure is downstream in Studio
	BlameNone   = "none"   // no CLI call was even recorded
)

// BlameReport is the failure-attribution summary printed into test output.
// With INFERYARD_E2E_ROOT set, the referenced stream files survive for
// inspection; otherwise the printed fields carry the same content.
type BlameReport struct {
	Verdict     string
	Command     string
	ExitCode    int
	Limitations []string
	StderrTail  string
	StdoutFile  string
	StderrFile  string
}

// analyzeFailure inspects the last recorded CLI call at the moment of
// failure and attributes the step to the CLI side or the Studio side.
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
	// Exit 0: Studio's contract says stdout is JSON. If it is not, the CLI
	// broke the wire; if it is, the boundary held and the failure is ours.
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
