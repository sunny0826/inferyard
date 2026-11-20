# InferYard CLI

English | [中文](README.zh-CN.md)

`inferyard` is the command-line execution subproject of
[InferYard Studio](../README.md). Studio provides the desktop interface and
manages model services; it invokes `inferyard` to run frozen task sets against
local models, record answer, latency, and resource evidence, and project the
`studio.json` report displayed in Studio. Quality, latency, and memory are
reported separately; when comparison preconditions are not met, the CLI explains
why instead of producing a single composite score.

## Start from the terminal

1. Follow the [installation guide](docs/installation.md) to install a pinned CLI version, initialize the operations directory and run the device preflight.
2. [Prepare a candidate for your platform](docs/installation.md#3-准备候选). Start the declared model service in another terminal, then bind its actual PID and local endpoint.
3. Run `zh-core` and `zh-svg-pelican` separately, verify each raw run, and generate one combined report. If the core run is already complete, add only the missing pelican run.
4. Open `studio.json` in Studio. For handoff, copy the sealed runs along with the report.

The desktop workflow starts on the [Studio task page](../studio/README.md#pages-and-first-launch). Studio can reuse an existing CLI and manage services it starts.

## Read more

[InferYard Studio](../README.md) · [Studio guide](../studio/README.md) ·
[Installation](docs/installation.md) ·
[Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

Run CLI development commands from `cli/` in this monorepo; root `mise` tasks
already enter that directory.

This repository starts a fresh Git history from the current InferYard source;
original runs, model assets, and the source project's history are not
distributed. Redacted public evidence can be produced with `public package`.
Licensed under [MIT](LICENSE).
