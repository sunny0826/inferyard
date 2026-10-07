package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"inferyard/observer/internal/observe"
)

func watch(ctx context.Context, o options, session, cwd string, stdout, stderr io.Writer) int {
	health, err := observe.NewHealthProbe(o.endpoint)
	if err != nil {
		return reject(stderr, err.Error(), 2)
	}
	target := observe.Target{PID: o.pid}
	var binding *observe.Binding
	diskPath, diskScope := cwd, "observer_cwd_filesystem"
	if o.root != "" {
		b, t, err := observe.ReadBinding(o.root)
		if err != nil {
			return reject(stderr, err.Error(), 2)
		}
		binding, target = &b, t
		diskPath, err = filepath.Abs(o.root)
		if err != nil {
			return reject(stderr, "benchmark_directory_unavailable", 2)
		}
		diskScope = "benchmark_run_filesystem"
	}
	raw, err := observe.NativeProcess(target.PID)
	if err != nil {
		return reject(stderr, "target_"+observe.ErrorReason(err), 2)
	}
	if target.StartTicks != 0 && target.StartTicks != raw.Target.StartTicks {
		return reject(stderr, "benchmark_process_identity_changed", 2)
	}
	target = raw.Target
	self, err := observe.NativeProcess(os.Getpid())
	if err != nil {
		return reject(stderr, "observer_identity_unavailable", 4)
	}
	var output *os.File
	if o.output != "" {
		if o.root != "" {
			inside, err := outputInsideRun(o.root, o.output)
			if err != nil {
				return reject(stderr, "output_parent_unavailable", 4)
			}
			if inside {
				return reject(stderr, "output_must_be_outside_benchmark_run", 2)
			}
		}
		output, err = os.OpenFile(o.output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return reject(stderr, "output_create_failed_or_already_exists", 4)
		}
		defer output.Close()
		stdout = output
	}
	header := observe.Header{Envelope: observe.Base("observer_header", session), Version: version,
		SourceHash: buildSource(), BinaryHash: binaryHash(), System: observe.NativeInventory(),
		IntervalNS: int64(o.interval), DurationNS: int64(o.duration), Targets: []observe.Target{target},
		Observer: self.Target, Binding: binding, Clock: "session_monotonic_ns_not_benchmark_clock",
		DiskScope: diskScope}
	start := time.Now()
	header.UTC = start.UTC().Format(time.RFC3339Nano)
	encoder := json.NewEncoder(stdout)
	if encoder.Encode(header) != nil {
		return reject(stderr, "header_write_failed", 4)
	}
	tracker, selfTracker := observe.NewTracker(), observe.NewTracker()
	end := observe.End{Envelope: observe.Base("observer_end", session), Reason: "duration_reached"}
	deadline, next := start.Add(o.duration), start
	code := 0
	for {
		waitUntil := next
		if waitUntil.After(deadline) {
			waitUntil = deadline
		}
		if delay := time.Until(waitUntil); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
			}
		}
		if ctx.Err() != nil {
			end.Reason, code = "cancelled", 130
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		sample := observe.Sample{Envelope: observe.Base("observer_sample", session),
			Seq: end.Samples + 1, ReadStartedNS: time.Since(start).Nanoseconds()}
		sample.Host = observe.NativeHost(diskPath)
		sample.Processes = []observe.Process{readProcess(start, target, tracker)}
		sample.Observer = readProcess(start, self.Target, selfTracker)
		sample.Health = health.Read(ctx, time.Now())
		sample.ReadFinishedNS = time.Since(start).Nanoseconds()
		if encoder.Encode(sample) != nil {
			return reject(stderr, "sample_write_failed", 4)
		}
		end.Samples++
		if sample.Processes[0].State != "running" {
			end.Reason, code = "target_unavailable", 3
			break
		}
		if sample.Observer.State != "running" {
			end.Reason, code = "observer_unavailable", 4
			break
		}
		next = next.Add(o.interval)
		if now := time.Now(); now.After(next) {
			missed := int64(now.Sub(next)/o.interval) + 1
			end.Skipped += missed
			next = next.Add(time.Duration(missed) * o.interval)
		}
	}
	end.ElapsedNS = time.Since(start).Nanoseconds()
	if encoder.Encode(end) != nil {
		return reject(stderr, "end_write_failed", 4)
	}
	if output != nil {
		if output.Sync() != nil || output.Close() != nil {
			return reject(stderr, "output_sync_or_close_failed", 4)
		}
	}
	return code
}

func outputInsideRun(root, output string) (bool, error) {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return false, err
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return false, err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(filepath.VolumeName(base), filepath.VolumeName(parent)) {
		return false, nil
	}
	relative, err := filepath.Rel(base, parent)
	if err != nil {
		return false, err
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) &&
		!filepath.IsAbs(relative), nil
}

func readProcess(start time.Time, target observe.Target, tracker *observe.Tracker) observe.Process {
	before := time.Since(start).Nanoseconds()
	raw, err := observe.NativeProcess(target.PID)
	after := time.Since(start).Nanoseconds()
	return tracker.Read(target, before, after, raw, err)
}
