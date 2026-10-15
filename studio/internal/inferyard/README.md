# CLI process boundary

`Client.Exec` passes argv directly, captures stdout/stderr, and separates CLI exit
status from launch, I/O and context errors. Nonzero CLI exits retain stdout.
Cancellation interrupts the child process group before the two-second kill fallback
on Unix; Windows terminates the child. No shell expands arguments.

`ParseVersion`, `Parse`, `ParseDeviceCheck` and `ParseProgress` decode CLI output.
`SplitCommand` splits quoted wizard arguments without executing substitutions.
The core service owns executable discovery, device refresh and workspace progress,
using its injectable executor and these parsers. Tests exercise the same boundary.

The sibling `evidence` package indexes layout2 runs and tails append-only JSONL
records. It does not certify evidence, infer benchmark scores or own downloads.
The service observes model download files together with task state in `core/downloads.go`.

Current responsibilities and verification boundaries live in the
[Studio subsystem reference](../../../docs/subsystems/studio-panel.md).
Run the checks documented in the [Studio README](../../README.md).
