# Core services

`Service` owns settings, device snapshots, the serial run queue, concurrent download
tasks, model service ownership, workspace progress and report creation. Native pages
call it directly and subscribe to its in-process `EventBus`. A replay gap causes the
view to reload its snapshot. The application context owns background work.

`Options.Execute` defaults to the CLI client; tests inject fixture executors. The CLI
owns binding, identity checks, evidence validation, resume eligibility and benchmark
execution. Core reads evidence for presentation and preserves missing measurements as
`null`; it does not certify a run or recalculate scores.

The [Studio subsystem reference](../../../docs/subsystems/studio-panel.md) is the
source for responsibilities, state transitions, path rules and current interfaces.
Build, test and native acceptance boundaries are documented in the
[Studio README](../../README.md). Historical Web server handoffs are superseded by
these references.
