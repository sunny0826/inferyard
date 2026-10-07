package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"inferyard/observer/internal/observe"
)

var version = "0.2.0"
var sourceHash = ""

const help = `inferyard-observer — 只读系统配置与进程监测，默认无网络
  inferyard-observer snapshot
  inferyard-observer watch --pid PID [--interval 1s] [--duration 30s] [--out NEW.jsonl]
  inferyard-observer watch --bench-run RUN_DIR [--duration 30s] [--out NEW.jsonl]
  watch 可显式加 --endpoint http://127.0.0.1:PORT，仅 GET /health
  --version / --help
候选名称不证明模型已加载；CPU 活跃不证明正在生成。
`

type options struct {
	pid                    int
	root, output, endpoint string
	interval, duration     time.Duration
}

func main() {
	// Return the documented IO code when a consumer closes stdout early.
	signal.Ignore(syscall.SIGPIPE)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(code)
}

func reject(stderr io.Writer, reason string, code int) int {
	fmt.Fprintln(stderr, reason)
	return code
}

func parse(args []string) (options, error) {
	var o options
	f := flag.NewFlagSet("watch", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.IntVar(&o.pid, "pid", 0, "target PID")
	f.StringVar(&o.root, "bench-run", "", "existing v3 run directory")
	f.StringVar(&o.output, "out", "", "new JSONL file")
	f.StringVar(&o.endpoint, "endpoint", "", "explicit loopback HTTP health endpoint")
	f.DurationVar(&o.interval, "interval", time.Second, "sample interval")
	f.DurationVar(&o.duration, "duration", 30*time.Second, "fixed duration")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return o, fmt.Errorf("watch_arguments_invalid")
	}
	if (o.pid > 0) == (o.root != "") || o.pid < 0 || o.pid > 2_147_483_647 ||
		o.interval < 100*time.Millisecond || o.interval > time.Minute ||
		o.duration < time.Second || o.duration > 24*time.Hour {
		return o, fmt.Errorf("watch_requires_one_target_and_bounded_timing")
	}
	return o, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "help")) {
		_, err := io.WriteString(stdout, help)
		if err != nil {
			return reject(stderr, "output_failed", 4)
		}
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		if json.NewEncoder(stdout).Encode(map[string]any{"tool": "inferyard-observer", "version": version,
			"schema_version": 3, "definition": observe.Definition}) != nil {
			return reject(stderr, "output_failed", 4)
		}
		return 0
	}
	if len(args) == 2 && args[0] == "watch" && args[1] == "--help" {
		return run(ctx, []string{"--help"}, stdout, stderr)
	}
	var o options
	var err error
	switch args[0] {
	case "snapshot":
		if len(args) != 1 {
			return reject(stderr, "snapshot_arguments_invalid", 2)
		}
	case "watch":
		o, err = parse(args[1:])
		if err != nil {
			return reject(stderr, err.Error(), 2)
		}
	default:
		return reject(stderr, "command_unknown", 2)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return reject(stderr, "session_id_failed", 4)
	}
	session := hex.EncodeToString(id[:])
	cwd, err := os.Getwd()
	if err != nil {
		return reject(stderr, "working_directory_unavailable", 4)
	}
	if args[0] == "snapshot" {
		candidates, e := observe.NativeDiscover()
		if candidates == nil {
			candidates = []observe.Target{}
		}
		s := observe.Snapshot{Envelope: observe.Base("observer_snapshot", session),
			System: observe.NativeInventory(), Host: observe.NativeHost(cwd), Candidates: candidates}
		if e != nil {
			reason := observe.ErrorReason(e)
			s.DiscoveryReason = &reason
		}
		if json.NewEncoder(stdout).Encode(s) != nil {
			return reject(stderr, "output_failed", 4)
		}
		return 0
	}
	return watch(ctx, o, session, cwd, stdout, stderr)
}

func binaryHash() observe.Metric[string] {
	const source = "sha256.current_executable_bytes"
	path, err := os.Executable()
	if err != nil {
		return observe.Missing[string]("executable_unavailable", source)
	}
	f, err := os.Open(path)
	if err != nil {
		return observe.Missing[string]("executable_unreadable", source)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return observe.Missing[string]("executable_not_regular", source)
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, f)
	after, err := f.Stat()
	current, pathErr := os.Stat(path)
	if copyErr != nil || err != nil || pathErr != nil || !os.SameFile(before, current) ||
		before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return observe.Missing[string]("executable_changed_or_unreadable", source)
	}
	return observe.Known(hex.EncodeToString(h.Sum(nil)), source)
}

func buildSource() observe.Metric[string] {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sourceHash) {
		return observe.Missing[string]("unbound_development_build", "build_receipt.source_sha256")
	}
	return observe.Known(sourceHash, "build_receipt.source_sha256")
}
