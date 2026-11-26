package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type cachedProgressEntry struct {
	signature string
	response  ProgressResponse
}

func appendFileSig(h interface{ Write([]byte) (int, error) }, label, path string) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintf(h, "%s|missing\n", label)
		} else {
			_, _ = fmt.Fprintf(h, "%s|err|%v\n", label, err)
		}
		return
	}
	_, _ = fmt.Fprintf(h, "%s|%d|%d|%d\n", label, info.Mode(), info.Size(), info.ModTime().UnixNano())
	if info.Mode()&os.ModeSymlink != 0 {
		if target, tErr := os.Stat(path); tErr == nil {
			_, _ = fmt.Fprintf(h, "%s->|%d|%d|%d\n", label, target.Mode(), target.Size(), target.ModTime().UnixNano())
		}
	}
}

func appendTreeSigs(h interface{ Write([]byte) (int, error) }, label, root string) bool {
	appendFileSig(h, label, root)
	if _, err := os.Lstat(root); err != nil {
		return os.IsNotExist(err)
	}
	ok := true
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			ok = false
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		appendFileSig(h, label+"/"+filepath.ToSlash(rel), path)
		return nil
	})
	return ok && err == nil
}

func appendDirChildrenSigs(h interface{ Write([]byte) (int, error) }, label, dir string, markers ...string) bool {
	appendFileSig(h, label, dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return os.IsNotExist(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		child := filepath.Join(dir, name)
		appendFileSig(h, label+"/"+name, child)
		if !entry.IsDir() {
			continue
		}
		for _, m := range markers {
			appendFileSig(h, label+"/"+name+"/"+m, filepath.Join(child, m))
		}
	}
	return true
}

func appendPreparationDirSigs(h interface{ Write([]byte) (int, error) }, label, dir string, refKeys ...string) bool {
	appendFileSig(h, label, dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return os.IsNotExist(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		child := filepath.Join(dir, name)
		appendFileSig(h, label+"/"+name, child)
		if !entry.IsDir() {
			continue
		}
		prepPath := filepath.Join(child, "preparation.json")
		appendFileSig(h, label+"/"+name+"/preparation.json", prepPath)
		raw, readErr := os.ReadFile(prepPath)
		if readErr != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		for _, k := range refKeys {
			if v, ok := doc[k].(string); ok && strings.TrimSpace(v) != "" {
				appendFileSig(h, label+"/"+name+"/@"+k, v)
			}
		}
	}
	return true
}

func appendResultsProgressSigs(h interface{ Write([]byte) (int, error) }, resultsDir string) bool {
	appendFileSig(h, "results", resultsDir)
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		return os.IsNotExist(err)
	}
	runMarkers := []string{"inputs.json", "manifest.json", "events.jsonl", "run.json"}
	for _, entry := range entries {
		name := entry.Name()
		child := filepath.Join(resultsDir, name)
		appendFileSig(h, "results/"+name, child)
		if !entry.IsDir() {
			continue
		}
		for _, m := range runMarkers {
			appendFileSig(h, "results/"+name+"/"+m, filepath.Join(child, m))
		}
		appendFileSig(h, "results/"+name+"/batch.json", filepath.Join(child, "batch.json"))
		appendFileSig(h, "results/"+name+"/plan.json", filepath.Join(child, "plan.json"))
		appendFileSig(h, "results/"+name+"/config.json", filepath.Join(child, "config.json"))
		appendFileSig(h, "results/"+name+"/frozen-plan/plan.json", filepath.Join(child, "frozen-plan", "plan.json"))
		appendFileSig(h, "results/"+name+"/frozen-plan/config.json", filepath.Join(child, "frozen-plan", "config.json"))
		runsSub := filepath.Join(child, "runs")
		if subEntries, subErr := os.ReadDir(runsSub); subErr == nil {
			appendFileSig(h, "results/"+name+"/runs", runsSub)
			for _, sub := range subEntries {
				subName := sub.Name()
				subChild := filepath.Join(runsSub, subName)
				appendFileSig(h, "results/"+name+"/runs/"+subName, subChild)
				if !sub.IsDir() {
					continue
				}
				for _, m := range runMarkers {
					appendFileSig(h, "results/"+name+"/runs/"+subName+"/"+m, filepath.Join(subChild, m))
				}
			}
		}
	}
	return true
}

func (s *Service) workspaceProgressSignature(u UserSettings, p InferyardProbe) (string, bool) {
	if p.Status != "ok" || p.Path == nil || *p.Path == "" {
		return "", false
	}
	w := workspaceRoot(u)
	if w == nil || *w == "" {
		return "", false
	}
	ws := filepath.Clean(*w)
	res := filepath.Join(ws, "results")
	if r := s.resultsRoot(u); r != nil && *r != "" {
		res = filepath.Clean(*r)
	}
	rep := filepath.Join(ws, "reports")
	if r := s.reportsRoot(u); r != nil && *r != "" {
		rep = filepath.Clean(*r)
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "cli|%s|%s|%s\n", *p.Path, str(p.Version), Version)
	appendFileSig(h, "cli-bin", *p.Path)
	appendFileSig(h, "ws", ws)
	appendFileSig(h, "ws/prep", filepath.Join(ws, "preparation.json"))
	if !appendDirChildrenSigs(h, "bundles", filepath.Join(ws, "bundles")) {
		return "", false
	}
	if !appendTreeSigs(h, "preflight", filepath.Join(ws, "preflight")) {
		return "", false
	}
	if !appendPreparationDirSigs(h, "candidates", filepath.Join(ws, "candidates"), "candidate", "bundle") {
		return "", false
	}
	if !appendPreparationDirSigs(h, "bound", filepath.Join(ws, "bound"), "candidate", "config") {
		return "", false
	}
	if !appendResultsProgressSigs(h, res) {
		return "", false
	}
	if !appendDirChildrenSigs(h, "reports", rep, "studio.json") {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// ProgressCached returns a cached WorkspaceProgress response when the CLI binary
// and all workspace/results/reports artifact metadata are unchanged, avoiding
// repeated Python subprocess spawns during UI refreshes.
func (s *Service) ProgressCached(ctx context.Context) (ProgressResponse, *ApiError) {
	u := s.userSettings()
	p := s.probe(u)
	if p.Status != "ok" {
		return ProgressResponse{}, cliProbeError(p)
	}
	if sig, ok := s.workspaceProgressSignature(u, p); ok {
		s.caches.mu.Lock()
		if s.caches.progress != nil && s.caches.progress.signature == sig {
			copied := clone(s.caches.progress.response)
			s.caches.mu.Unlock()
			return copied, nil
		}
		s.caches.mu.Unlock()
	}

	s.progressExecMu.Lock()
	defer s.progressExecMu.Unlock()
	preSig, preOK := s.workspaceProgressSignature(u, p)
	if preOK {
		s.caches.mu.Lock()
		if s.caches.progress != nil && s.caches.progress.signature == preSig {
			copied := clone(s.caches.progress.response)
			s.caches.mu.Unlock()
			return copied, nil
		}
		s.caches.mu.Unlock()
	}

	resp, apiErr := s.Progress(ctx)
	if apiErr != nil || resp.ExitCode != inferyard.ExitComplete || resp.Progress == nil {
		return resp, apiErr
	}
	if postSig, postOK := s.workspaceProgressSignature(u, p); preOK && postOK && preSig == postSig {
		s.caches.mu.Lock()
		s.caches.progress = &cachedProgressEntry{
			signature: postSig,
			response:  clone(resp),
		}
		s.caches.mu.Unlock()
	}
	return resp, nil
}
