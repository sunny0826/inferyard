package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"unicode/utf8"
)

type EngineCheck struct {
	Eligible          bool
	Reason            *string
	ReportedVersion   *string
	ExpectedRelease   string
	Modes             *EngineCheckModes
	Mode              *string
	Limitations       []string
	Status            string
	Family            *string
	DeclarationSHA256 *string
	VerificationScope string
}

type EngineCheckModes struct {
	CPU   bool `json:"cpu"`
	Metal bool `json:"metal"`
}

var engineCheckReasons = []string{
	"invalid_library_manifest",
	"unsupported_engine_build_requires_adapter_validation",
	"unsupported_runtime_profile",
	"runtime_receipt_invalid",
	"runtime_profile_mismatch",
	"runtime_asset_mismatch",
}

var engineCheckLimitations = []string{
	"invalid_input",
	"unsupported_platform",
	"engine_version_query_failed",
	"package_resource_invalid",
	"io_error",
	"internal_error",
	"cancelled",
}

func ParseEngineCheck(raw []byte, exitCode int) (EngineCheck, error) {
	var result EngineCheck
	invalid := errors.New("invalid engine check result")
	if !utf8.Valid(raw) {
		return result, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(d); err != nil {
		return result, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return result, invalid
	}
	r, err := Parse(raw)
	if err != nil || r.Command != "engine check" || r.RunID != nil || r.EvidenceDir != nil || r.Limitations == nil {
		return result, invalid
	}
	if !discoveryFields(raw, "command", "status", "completeness", "run_id", "evidence_dir", "limitations", "details") {
		return result, invalid
	}
	for _, code := range r.Limitations {
		if !slices.Contains(engineCheckLimitations, code) {
			return result, invalid
		}
	}
	if exitCode == 2 || exitCode == 130 || exitCode == 4 {
		status := "blocked"
		if exitCode == 130 {
			status = "interrupted"
		} else if exitCode == 4 {
			status = "error"
		}
		if r.Status != status || r.Completeness != "incomplete" || len(r.Limitations) == 0 || !bytes.Equal(bytes.TrimSpace(r.Details), []byte("null")) {
			return result, invalid
		}
		if exitCode == 130 && (len(r.Limitations) != 1 || r.Limitations[0] != "cancelled") {
			return result, invalid
		}
		return EngineCheck{Limitations: r.Limitations, Status: status}, nil
	}
	if exitCode != 0 {
		return result, invalid
	}
	if r.Status != "checked" || r.Completeness != "complete" || len(r.Limitations) != 0 {
		return result, invalid
	}
	var branch map[string]json.RawMessage
	if json.Unmarshal(r.Details, &branch) != nil {
		return result, invalid
	}
	if _, ok := branch["definition"]; ok {
		return parseLocalEngineCheck(r.Details)
	}
	if !discoveryFields(r.Details, "eligible", "reason", "reported_version", "expected_release", "modes", "mode") {
		return result, invalid
	}
	var details struct {
		Eligible        bool              `json:"eligible"`
		Reason          *string           `json:"reason"`
		ReportedVersion *string           `json:"reported_version"`
		ExpectedRelease string            `json:"expected_release"`
		Modes           *EngineCheckModes `json:"modes"`
		Mode            *string           `json:"mode"`
	}
	if err := json.Unmarshal(r.Details, &details); err != nil {
		return result, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.Details, &fields) != nil {
		return result, invalid
	}
	if !bytes.Equal(fields["eligible"], []byte("true")) && !bytes.Equal(fields["eligible"], []byte("false")) {
		return result, invalid
	}
	if details.Modes != nil {
		if !discoveryFields(fields["modes"], "cpu", "metal") {
			return result, invalid
		}
		var modes map[string]json.RawMessage
		if json.Unmarshal(fields["modes"], &modes) != nil {
			return result, invalid
		}
		for _, key := range []string{"cpu", "metal"} {
			raw := bytes.TrimSpace(modes[key])
			if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
				return result, invalid
			}
		}
	}
	if details.ExpectedRelease == "" {
		return result, invalid
	}
	if details.Eligible {
		if details.Reason != nil || details.ReportedVersion == nil || *details.ReportedVersion == "" {
			return result, invalid
		}
		switch {
		case details.Modes != nil && details.Mode == nil:
		case details.Modes == nil && details.Mode != nil && slices.Contains([]string{"cpu", "cuda"}, *details.Mode):
		default:
			return result, invalid
		}
	} else {
		if details.Reason == nil || !slices.Contains(engineCheckReasons, *details.Reason) ||
			details.ReportedVersion != nil || details.Modes != nil || details.Mode != nil {
			return result, invalid
		}
	}
	return EngineCheck{
		Eligible:        details.Eligible,
		Reason:          details.Reason,
		ReportedVersion: details.ReportedVersion,
		ExpectedRelease: details.ExpectedRelease,
		Modes:           details.Modes,
		Mode:            details.Mode,
		Limitations:     []string{},
		Status:          r.Status,
	}, nil
}

func parseLocalEngineCheck(raw []byte) (EngineCheck, error) {
	invalid := errors.New("invalid local engine check result")
	var d struct {
		Definition        string  `json:"definition"`
		Eligible          *bool   `json:"eligible"`
		Reason            *string `json:"reason"`
		Family            *string `json:"family"`
		DeclarationSHA256 *string `json:"declaration_sha256"`
		Scope             string  `json:"verification_scope"`
		Requests          *int    `json:"model_requests_sent"`
		Ready             *bool   `json:"ready_to_run"`
	}
	if !discoveryFields(raw, "definition", "eligible", "reason", "family", "declaration_sha256", "verification_scope", "model_requests_sent", "ready_to_run") ||
		json.Unmarshal(raw, &d) != nil || d.Definition != "local-engine-check.v2" || d.Eligible == nil || d.Ready == nil || *d.Ready ||
		d.Requests == nil || *d.Requests != 0 || d.Scope != "installation_runtime_closure_only" ||
		(d.Family != nil && !localFamily(*d.Family)) {
		return EngineCheck{}, invalid
	}
	if *d.Eligible {
		if d.Reason != nil || d.Family == nil || d.DeclarationSHA256 == nil || !candidateHash(*d.DeclarationSHA256) {
			return EngineCheck{}, invalid
		}
	} else if d.Reason == nil || *d.Reason == "" || d.DeclarationSHA256 != nil {
		return EngineCheck{}, invalid
	}
	return EngineCheck{Eligible: *d.Eligible, Reason: d.Reason, Family: d.Family, DeclarationSHA256: d.DeclarationSHA256, VerificationScope: d.Scope, Limitations: []string{}, Status: "checked"}, nil
}
