package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	EngineCheckChecking   = "checking"
	EngineCheckEligible   = "eligible"
	EngineCheckIneligible = "ineligible"
	EngineCheckUnknown    = "unknown"
)

type EngineEligibility struct {
	Family            string          `json:"family,omitempty"`
	DeclarationSHA256 *string         `json:"declaration_sha256,omitempty"`
	VerificationScope string          `json:"verification_scope,omitempty"`
	State             string          `json:"state"`
	Reason            string          `json:"reason"`
	Version           string          `json:"version"`
	Release           string          `json:"release"`
	Modes             map[string]bool `json:"modes,omitempty"`
	Mode              string          `json:"mode,omitempty"`
}

type EngineCheckOverview struct {
	Supported bool                         `json:"supported"`
	Checking  int                          `json:"checking"`
	Engines   map[string]EngineEligibility `json:"engines"`
}

type engineCheckEntry struct {
	key      string
	verified bool
	failedAt time.Time
	result   EngineEligibility
}

type engineCheckTarget struct {
	name, path, receipt, key string
	installation             string
	cliPath                  string
	generation               uint64
}

func engineCheckArgs(path, receipt string) []string {
	if runtime.GOOS == "windows" {
		return []string{"engine", "check", "--runtime-receipt", receipt}
	}
	return []string{"engine", "check", "--engine", path}
}

func libraryFingerprint(enginePath string) string {
	entries, err := os.ReadDir(filepath.Dir(enginePath))
	if err != nil {
		return ""
	}
	extension := ".dylib"
	switch runtime.GOOS {
	case "windows":
		extension = ".dll"
	case "linux":
		extension = ".so"
	}
	records := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if extension == ".so" {
			if !strings.Contains(name, ".so") {
				continue
			}
		} else if !strings.HasSuffix(strings.ToLower(name), extension) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			path := filepath.Join(filepath.Dir(enginePath), name)
			target, err := os.Readlink(path)
			record := fmt.Sprintf("%s|link|%s|%v", name, target, err)
			if info, err := os.Stat(path); err == nil {
				record += fmt.Sprintf("|%d|%d", info.Size(), info.ModTime().UnixNano())
			} else {
				record += "|dangling"
			}
			records = append(records, record)
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		records = append(records, fmt.Sprintf("%s|%d|%d", name, info.Size(), info.ModTime().UnixNano()))
	}
	sort.Strings(records)
	h := sha256.New()
	for _, record := range records {
		h.Write([]byte(record))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func engineCheckKey(path, receipt string, modified time.Time) string {
	stamp := "missing"
	if info, err := os.Stat(receipt); err == nil {
		stamp = fmt.Sprintf("%d|%d", info.ModTime().UnixNano(), info.Size())
	}
	return fmt.Sprintf("%d|%s|%s|%s", modified.UnixNano(), libraryFingerprint(path), receipt, stamp)
}

func (s *Service) checkEngineWithCLI(ctx context.Context, cliPath, path, receipt string) (inferyard.EngineCheck, *ApiError) {
	result, failure := s.checkEngineArgsWithCLI(ctx, cliPath, engineCheckArgs(path, receipt))
	if failure == nil && result.VerificationScope != "" {
		return inferyard.EngineCheck{}, Err("engine_check_invalid_response")
	}
	return result, failure
}

func (s *Service) checkInstallationWithCLI(ctx context.Context, cliPath, path string) (inferyard.EngineCheck, *ApiError) {
	result, failure := s.checkEngineArgsWithCLI(ctx, cliPath, []string{"engine", "check", "--installation", path})
	if failure == nil && result.VerificationScope != "installation_runtime_closure_only" {
		return inferyard.EngineCheck{}, Err("engine_check_invalid_response")
	}
	if failure == nil && !result.Eligible {
		result.Reason = ptr(localInstallationReason(str(result.Reason)))
	}
	return result, failure
}

func (s *Service) checkEngineArgsWithCLI(ctx context.Context, cliPath string, args []string) (inferyard.EngineCheck, *ApiError) {
	out, err := s.exec(ctx, cliPath, args...)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
			return inferyard.EngineCheck{}, Err("cli_outdated")
		}
		return inferyard.EngineCheck{}, Err("engine_check_error")
	}
	if out.ExitCode == inferyard.ExitBlocked {
		var header struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(out.Stdout, &header) != nil || header.Command != "engine check" {
			return inferyard.EngineCheck{}, Err("cli_outdated")
		}
	}
	result, err := inferyard.ParseEngineCheck(out.Stdout, out.ExitCode)
	if err != nil {
		return inferyard.EngineCheck{}, Err("engine_check_invalid_response")
	}
	if out.ExitCode == inferyard.ExitBlocked || out.ExitCode == inferyard.ExitCancelled {
		code := "cli_blocked"
		if out.ExitCode == inferyard.ExitCancelled {
			code = "cancelled"
		}
		e := Err(code)
		e.Limitations = result.Limitations
		return result, e
	}
	if out.ExitCode == inferyard.ExitToolError {
		return result, &ApiError{Code: "engine_check_error", Limitations: result.Limitations}
	}
	return result, nil
}

func (s *Service) VerifyEngines() {
	data := s.registry()
	p := s.probe(s.userSettings())
	cliKey := str(p.Path) + "|" + str(p.Version) + "|" + p.Status
	var todo []engineCheckTarget
	s.checkMu.Lock()
	if cliKey != s.checkCLI {
		s.checkCLI = cliKey
		s.checkGeneration++
		s.checks = map[string]engineCheckEntry{}
		s.checkInflight = map[string]bool{}
		s.checkSupported = true
	}
	targets := make([]engineCheckTarget, 0, len(data.engines))
	for _, e := range data.engines {
		var key string
		if e.installation != "" {
			key = installationKey(e.installation)
		} else {
			key = engineCheckKey(e.path, e.receipt, e.modified)
		}
		targets = append(targets, engineCheckTarget{
			name: e.engine.Name, path: e.path, receipt: e.receipt,
			installation: e.installation,
			cliPath:      str(p.Path), generation: s.checkGeneration,
			key: key,
		})
	}
	s.checkNames = targets
	if p.Status != "ok" {
		s.checkSupported = false
	}
	if s.checkSupported {
		for _, t := range targets {
			entry, ok := s.checks[t.path]
			if ok && entry.key == t.key {
				if entry.verified || time.Since(entry.failedAt) < s.checkRetry {
					continue
				}
			}
			if s.checkInflight[t.path] {
				continue
			}
			s.checkInflight[t.path] = true
			todo = append(todo, t)
		}
	}
	s.checkMu.Unlock()
	for _, t := range todo {
		go s.runEngineCheck(t)
	}
}

func (s *Service) runEngineCheck(t engineCheckTarget) {
	s.checkSem <- struct{}{}
	defer func() { <-s.checkSem }()
	s.checkMu.Lock()
	if t.generation != s.checkGeneration {
		s.checkMu.Unlock()
		return
	}
	if !s.checkSupported {
		delete(s.checkInflight, t.path)
		s.checkMu.Unlock()
		return
	}
	s.checkMu.Unlock()
	eligibility := EngineEligibility{State: EngineCheckUnknown, Reason: "engine_check_error"}
	verified := false
	if t.installation == "" && runtime.GOOS == "windows" && t.receipt == "" {
		eligibility.Reason = "runtime_receipt_missing"
	} else {
		timeout := 30 * time.Second
		if t.installation != "" {
			timeout = installationCheckTimeout
		}
		ctx, cancel := context.WithTimeout(s.ctx, timeout)
		var result inferyard.EngineCheck
		var e *ApiError
		if t.installation != "" {
			result, e = s.checkInstallationWithCLI(ctx, t.cliPath, t.installation)
		} else {
			result, e = s.checkEngineWithCLI(ctx, t.cliPath, t.path, t.receipt)
		}
		cancel()
		switch {
		case e == nil && result.Eligible:
			eligibility = EngineEligibility{State: EngineCheckEligible, Version: str(result.ReportedVersion), Release: result.ExpectedRelease, Mode: str(result.Mode)}
			if result.Modes != nil {
				eligibility.Modes = map[string]bool{"cpu": result.Modes.CPU, "metal": result.Modes.Metal}
			}
			verified = true
		case e == nil:
			eligibility = EngineEligibility{State: EngineCheckIneligible, Reason: str(result.Reason), Release: result.ExpectedRelease}
			verified = true
		case e.Code == "cli_outdated":
			eligibility = EngineEligibility{State: EngineCheckUnknown, Reason: "cli_outdated"}
		default:
			eligibility = EngineEligibility{State: EngineCheckUnknown, Reason: e.Code}
			if len(e.Limitations) > 0 {
				eligibility.Reason = e.Limitations[0]
			}
		}
		if t.installation != "" {
			eligibility.Family = str(result.Family)
			eligibility.DeclarationSHA256 = result.DeclarationSHA256
			eligibility.VerificationScope = "installation_runtime_closure_only"
		}
	}
	s.checkMu.Lock()
	if t.generation != s.checkGeneration {
		s.checkMu.Unlock()
		return
	}
	if eligibility.Reason == "cli_outdated" {
		s.checkSupported = false
	}
	delete(s.checkInflight, t.path)
	if eligibility.Reason != "cli_outdated" {
		entry := engineCheckEntry{key: t.key, verified: verified, result: eligibility}
		if !verified {
			entry.failedAt = time.Now()
		}
		s.checks[t.path] = entry
	}
	s.checkMu.Unlock()
	s.Bus.Publish("engine.check", map[string]any{"engine": t.name, "eligibility": eligibility})
}

func (s *Service) captureCheckGeneration(p InferyardProbe) (string, uint64) {
	cliKey := str(p.Path) + "|" + str(p.Version) + "|" + p.Status
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	if s.checks == nil {
		s.checks = map[string]engineCheckEntry{}
	}
	if s.checkInflight == nil {
		s.checkInflight = map[string]bool{}
	}
	if s.checkCLI == "" {
		s.checkCLI = cliKey
		s.checkSupported = true
	} else if s.checkCLI != cliKey {
		s.checkCLI = cliKey
		s.checkGeneration++
		s.checks = map[string]engineCheckEntry{}
		s.checkInflight = map[string]bool{}
		s.checkSupported = true
	}
	return cliKey, s.checkGeneration
}

func (s *Service) cacheInstallationCheck(cliKey string, generation uint64, path, key string, result inferyard.EngineCheck) {
	if !result.Eligible {
		return
	}
	eligibility := EngineEligibility{
		State:             EngineCheckEligible,
		Version:           str(result.ReportedVersion),
		Release:           result.ExpectedRelease,
		Mode:              str(result.Mode),
		Family:            str(result.Family),
		DeclarationSHA256: result.DeclarationSHA256,
		VerificationScope: "installation_runtime_closure_only",
	}
	if result.Modes != nil {
		eligibility.Modes = map[string]bool{"cpu": result.Modes.CPU, "metal": result.Modes.Metal}
	}
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	if s.checkCLI != cliKey || s.checkGeneration != generation || s.checkInflight[path] {
		return
	}
	if s.checks == nil {
		s.checks = map[string]engineCheckEntry{}
	}
	s.checks[path] = engineCheckEntry{key: key, verified: true, result: eligibility}
}

func (s *Service) EngineChecks() EngineCheckOverview {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	out := EngineCheckOverview{Supported: s.checkSupported, Engines: map[string]EngineEligibility{}}
	for _, n := range s.checkNames {
		entry, ok := s.checks[n.path]
		switch {
		case ok && entry.key == n.key:
			out.Engines[n.name] = entry.result
		case s.checkInflight[n.path] || s.checkSupported:
			out.Engines[n.name] = EngineEligibility{State: EngineCheckChecking}
			out.Checking++
		default:
			out.Engines[n.name] = EngineEligibility{State: EngineCheckUnknown, Reason: "cli_outdated"}
		}
	}
	return out
}
