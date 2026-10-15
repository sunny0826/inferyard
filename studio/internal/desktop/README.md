# Desktop notifications

`Subscribe` consumes the core EventBus replay ring on a background goroutine.
It maps terminal `run.state` and `download.state` events, plus `report.created`, to
fixed titles and bodies. Task paths and CLI output never enter notification content.
`main` starts delivery in MyGo `WhenReady`; the application has no headless mode.
Delivery errors and replay overruns are logged without changing task outcomes.

`TestEventNotifications` publishes events through the real bus and checks terminal
messages, ignored nonterminal events, redaction and delivery failures. It does not
prove OS notification delivery. macOS notification arrival and native Windows/Linux
execution remain separate acceptance work in the [backlog](../../../cli/docs/backlog.md).

Current build and verification commands live in the [Studio README](../../README.md).
