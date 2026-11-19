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
3. Follow the [default benchmark workflow](docs/guides/onboarding-and-scope.md#默认测评范围) to run the core and pelican bundles separately, verify each raw run and generate one report. If the core run is already complete, add only the missing pelican run.
4. Open `studio.json` in Studio. For handoff, copy the sealed runs along with the report. See the [workspace and delivery layout](docs/guides/onboarding-and-scope.md#工作区与交付目录组织).

The desktop workflow starts on the [Studio task page](../studio/README.md#pages-and-first-launch). Studio can reuse an existing CLI and manage services it starts. See [platform status](docs/platforms.md) for implementation scope, tested versions and outstanding native checks.

## Read more

[InferYard Studio](../README.md) · [Studio guide](../studio/README.md) ·
[Usage guide](docs/usage.md) · [CLI reference](docs/cli-surface.md) ·
[Reports](docs/reports.md) · [Documentation map](docs/README.md) ·
[Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

Run CLI development commands from `cli/` in this monorepo; root `mise` tasks
already enter that directory.

This repository starts a fresh Git history from the current InferYard source;
original runs, model assets, and the source project's history are not
distributed. Redacted public evidence can be produced with `public package`;
conclusion scope follows the
[evidence lineage rules](docs/data-contract.md#%E8%AF%81%E6%8D%AE%E8%A1%80%E7%BC%98%E4%B8%8E%E6%AF%94%E8%BE%83%E7%BB%93%E8%AE%BA).
Licensed under [MIT](LICENSE).
