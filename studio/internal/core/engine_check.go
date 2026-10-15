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

// Engine eligibility states shown by the models page picker and the Dashboard
// candidate stage. "unknown" covers an outdated CLI, a failed check and a
// check that has not finished yet; it never claims the engine is unusable,
// and only an outdated CLI leaves the engine selectable (config create is the
// final gate).
const (
	EngineCheckChecking   = "checking"
	EngineCheckEligible   = "eligible"
	EngineCheckIneligible = "ineligible"
	EngineCheckUnknown    = "unknown"
)

type EngineEligibility struct {
	State   string          `json:"state"`
	Reason  string          `json:"reason"`
	Version string          `json:"version"`
	Release string          `json:"release"`
	Modes   map[string]bool `json:"modes,omitempty"`
	Mode    string          `json:"mode,omitempty"`
}

// EngineCheckOverview is the UI-facing snapshot of the verification pipeline.
// Engines is keyed by registry display name; paths never leave core.
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
	cliPath                  string
	generation               uint64
}

// engineCheckArgs builds the CLI-C invocation for the platform: macOS checks
// the engine binary, Windows checks the runtime receipt of the prepared
// runtime (see cli-surface 引擎核验契约). Other platforms still invoke with
// --engine so the CLI can answer unsupported_platform honestly.
func engineCheckArgs(path, receipt string) []string {
	if runtime.GOOS == "windows" {
		return []string{"engine", "check", "--runtime-receipt", receipt}
	}
	return []string{"engine", "check", "--engine", path}
}

// libraryFingerprint hashes the sibling dynamic-library set (name, size,
// mtime). The CLI library-manifest rules are sensitive to sibling files, so a
// verdict cached on the engine mtime alone would misjudge a repaired engine
// forever. darwin uses *.dylib, windows *.dll, other platforms *.so*.
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

// CheckEngine runs one CLI `engine check`, following the same cli_outdated
// and parsing pattern as Discover.
func (s *Service) CheckEngine(ctx context.Context, path, receipt string) (inferyard.EngineCheck, *ApiError) {
	p := s.probe(s.userSettings())
	if p.Status != "ok" {
		return inferyard.EngineCheck{}, Err("cli_outdated")
	}
	return s.checkEngineWithCLI(ctx, *p.Path, path, receipt)
}

func (s *Service) checkEngineWithCLI(ctx context.Context, cliPath, path, receipt string) (inferyard.EngineCheck, *ApiError) {
	out, err := s.exec(ctx, cliPath, engineCheckArgs(path, receipt)...)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
			return inferyard.EngineCheck{}, Err("cli_outdated")
		}
		return inferyard.EngineCheck{}, Err("engine_check_error")
	}
	if out.ExitCode == 2 {
		var header struct {
			Command string `json:"command"`
		}
		// Old CLIs reject the unknown command with command "unknown".
		if json.Unmarshal(out.Stdout, &header) != nil || header.Command != "engine check" {
			return inferyard.EngineCheck{}, Err("cli_outdated")
		}
	}
	result, err := inferyard.ParseEngineCheck(out.Stdout, out.ExitCode)
	if err != nil {
		return inferyard.EngineCheck{}, Err("engine_check_invalid_response")
	}
	if out.ExitCode == 2 || out.ExitCode == 130 {
		code := "cli_blocked"
		if out.ExitCode == 130 {
			code = "cancelled"
		}
		e := Err(code)
		e.Limitations = result.Limitations
		return result, e
	}
	if out.ExitCode == 4 {
		return result, &ApiError{Code: "engine_check_error", Limitations: result.Limitations}
	}
	return result, nil
}

// VerifyEngines reconciles the check cache with a fresh registry scan:
// engines whose cache key (path + engine mtime + sibling library fingerprint
// + receipt) has no definitive verdict are checked in the background with
// bounded concurrency and a per-check timeout. Results arrive over the
// "engine.check" bus event. An outdated CLI flips Supported off instead of
// flooding errors.
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
		targets = append(targets, engineCheckTarget{
			name: e.engine.Name, path: e.path, receipt: e.receipt,
			cliPath: str(p.Path), generation: s.checkGeneration,
			key: engineCheckKey(e.path, e.receipt, e.modified),
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
				// Definitive verdicts are final for this key; transient
				// failures retry only after the backoff so a failing engine
				// cannot spin the verify → event → refresh loop.
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
	if runtime.GOOS == "windows" && t.receipt == "" {
		// Windows checks verify the runtime receipt; a bare binary directory
		// without one cannot be checked (real-machine behavior unverified).
		eligibility.Reason = "runtime_receipt_missing"
	} else {
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		result, e := s.checkEngineWithCLI(ctx, t.cliPath, t.path, t.receipt)
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
	// A binary or sibling library replaced during the check changes the key on
	// the next scan, which schedules a fresh verification.
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

// EngineChecks snapshots the current overview without touching the filesystem.
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
