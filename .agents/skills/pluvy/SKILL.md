---
name: pluvy
description: Run the pluvy gate after code tasks only in repositories with a project policy or an explicit project requirement, or when the user explicitly requests a run. Use also to adopt, install, configure, or explain pluvy, edit `.pluvy/pluvy.json`, triage findings, inspect checker coverage and options, or use approved line suppression with a reason in every supported language. Global installation or skill availability does not enable a repository; without project adoption or an explicit run request, skip automatic runs and gate reporting.
---

# pluvy

pluvy is a read-only, parse-based architecture policy gate. It audits this
repository's sources against the policy in `.pluvy/pluvy.json` and
reports findings without fixing anything: no mode modifies, stashes, stages,
restores, deletes, or rewrites source files, and the tool never changes Git
configuration.

## Decide whether the gate applies

Before any automatic post-task run or gate report, check the target repository
(the explicit `--root`, or the repository being worked on). The gate applies when
at least one of these is true:

- The repository has `.pluvy/pluvy.json`.
- Project instructions, scripts or CI explicitly require the gate, including a
  policy selected through `--config`. Use that project's root and config path.
- The user explicitly requests a gate run for this task.

A globally installed executable, a discoverable skill, an example policy or a
skill pointer alone does not enable the gate. Do not run the CLI to discover
whether the repository has adopted it. If none of the conditions applies, stop
this gate workflow silently: do not run it, create a policy, suggest setup, or
include a skipped, missing-config or "not counted as passed" gate notice in the
task report. Continue the project's own checks normally.

Keep missing or invalid configuration distinct from non-adoption. If the project
requires the gate or the user requests a run, missing configuration remains a
failure. An invalid or unreadable policy also remains a failure; do not silently
skip it or change the CLI's exit-code semantics.

Use this skill for adoption, installation, configuration, explanation and triage
when requested, even before adoption. Loading it for those purposes does not by
itself require a gate run or authorize creating a project policy.

## Gate policy (binding)

1. `After completing a code task in a repository where the gate applies, run the pluvy gate before reporting the task done, and report the mode, exit code and findings truthfully.`
2. `When the gate applies, never claim it passed when it was not run, failed with exit code 2, or the tool is not installed; state the reason explicitly instead. When it does not apply, omit gate execution and reporting.`
3. `Do not add suppressions or weaken the policy to make the gate appear green; policy changes require a separate full audit and review.`

## Post-task gate commands

| Situation | Command | Notes |
|---|---|---|
| Task changes are uncommitted (working tree, staged/unstaged/untracked) | `pluvy --base HEAD` | Compares HEAD with the working tree, including untracked files (new files get a full audit) |
| The task produced commits | `pluvy --base <task start commit>` (the parent of the task's first commit) | On a clean tree, `pluvy --base HEAD` audits 0 files and exits 0 — a silent false pass that must be avoided |
| The task start commit is unknown | `pluvy --all`, and state that this is a full audit that may include pre-existing debt | Prefer over-reporting to under-reporting |
| `--staged` | Not used for post-task gating | It only reads index blobs, which does not match the agent workflow boundary |

Canonical commands:

```sh
pluvy --all
pluvy --staged
pluvy --base <commit>
pluvy --root /path/to/project --all
pluvy --all --format json
```

`--root` defaults to the current directory (Git modes require the repository
root); `--config` defaults to `.pluvy/pluvy.json`, resolved relative
to the root.

## Exit codes

| Exit code | Meaning |
|---|---|
| `0` | No error-severity findings (warnings do not affect it) |
| `1` | Error-severity findings are present |
| `2` | Configuration/encoding/filesystem/Git failure (including a staged policy change, until a separate `--all` full audit passes and the change is committed) |

## Post-run checks

1. The gate output ends with a line like `Architecture base: N files`. If N is 0
   while the task really changed code, the base choice is wrong: reselect the
   base and rerun; do not treat the run as a pass.
2. If the task changed the policy file, do not judge the task by a `--base` run
   alone: run a separate `pluvy --all` and have the policy change reviewed.
3. Unrelated dirty changes that existed before the task are swept into
   `pluvy --base HEAD` runs: list them truthfully in the report; do not
   filter them silently.

## Report format

Apply this section only when the gate applies under the decision above. Report
the mode you ran, the exit code, and the findings (error/warning counts plus the
key items). If an applicable gate was not run, exited with code 2, or the tool is
not installed, state that explicitly — never report the task as gated or passed.
For a repository where the gate does not apply, omit gate status entirely. Keep
test-suite execution and its reporting independent of gate applicability.

## Triage

1. Fix the code first: findings are evidence of policy violations.
2. Every supported language (Python, Go, Java and the JavaScript family): a
   standalone next-line suppression comment naming the exact rule id is allowed
   only for an exception that received explicit (human) approval beforehand —
   the approval precedes and is independent of the reason; the written reason
   only records that approval. Use the language's own line-comment marker: `#`
   in Python, `//` in Go, Java and the JavaScript family. Block-comment forms,
   wildcards, lists, `type.suppression` itself and a missing reason are invalid
   and report `type.suppression`. Syntax and validation rules:
   [approved next-line diagnostic exceptions](references/comment-suppressions.md).
3. Policy changes (thresholds, excludes, owners, boundaries) are architecture
   decisions: they require a separate `--all` full audit and review. Never
   weaken the policy to force a green report (policy item 3).

## Capability boundaries and language support

The gate is read-only and parse-based: it parses the selected sources with
tree-sitter, collects syntax evidence and applies the policy. No mode fixes,
stashes, stages, restores, deletes or rewrites source files.

Six analyzers cover seven languages:

| Analyzer | Source suffixes |
|---|---|
| Python | `.py`, `.pyi` |
| Go | `.go` |
| Java | `.java` |
| JavaScript and JSX | `.js`, `.jsx`, `.mjs`, `.cjs` |
| TypeScript | `.ts`, `.mts`, `.cts` |
| TSX | `.tsx` |

Every checker is enabled by default for every selected file. `enabled: false`
turns one checker off for all selected files; there is no `enabled` flag scoped
to one language or one file. The global `include` and `exclude` lists drop files
from every checker at once, except that changed snapshot paths reported by the
run-level `test.deletion` gate follow the changed set instead (see [its checker
page](references/checkers/test.deletion.md)). Architecture `placements`,
`boundaries` and `paths` only select where that one architecture check applies.
`owners` is not a file selector: once owners are configured and the checker is
enabled, every selected
file must match exactly one owner, so an unmatched or doubly matched file is a
finding. Coverage differences therefore come from the diagnostics each analyzer
registers.

What the gate does not do:

- It does not infer types. Broad markers, annotations and conversions are
  syntactic evidence, not resolved types or compiler proofs.
- It does not resolve package aliases, build-system aliases, dynamic imports,
  reflection or computed members, and it does not prove runtime ownership or
  which implementation a call reaches.
- It does not detect all unused code, prove validators or logging safety, or
  measure coverage. Unused findings describe the checked snapshot view and
  unresolved external usage, not a repository-wide proof.
- It is not a linter, formatter, type checker or coverage tool. Keep the
  language formatter, type checker, tests, branch coverage and semantic
  architecture review as separate required checks.
- JavaScript and JSX have no type annotations to check: type safety needs a
  configured type-aware checker, even though usage, boundary and encoding
  analysis still run on those files.
- A passing syntax gate does not certify compliance with every policy rule.

## Checkers and per-language coverage

The schema has 22 logical checkers, which produce 29 built-in diagnostic IDs.
`function_size` owns the five `size.*` IDs, `test.integrity` owns
`test.tautology` and `test.vacuous`, `flow.handling` owns `flow.swallow` and
`flow.fallback`, and `type.contract` owns `type.nullable` and `type.union`;
the run-level `test.deletion` owns its single diagnostic outside analyzer
registration, and every other checker name matches its diagnostic ID.

| Checker | Reports | Languages |
|---|---|---|
| [`structure.guarded_projection`](references/checkers/structure.guarded_projection.md) | Repeated guarded field copies in one local region (three distinct destination fields by default) | All |
| [`function_size`](references/checkers/function_size.md) | Function lines, statements, complexity, nesting and parameters; one switch for five diagnostics | All |
| [`test.integrity`](references/checkers/test.integrity.md) | Tautological assertions and tests without a meaningful assertion; one switch for two diagnostics | All |
| [`test.deletion`](references/checkers/test.deletion.md) | Removed test files, assertion erosion and changed snapshot paths in changed audits; one run-level diagnostic with error and warning forms | All; run-level |
| [`flow.handling`](references/checkers/flow.handling.md) | Swallowed exception handlers and long `??` chains whose defaults are all bare literals; one switch for two diagnostics | Python, Java, JavaScript, JSX, TypeScript, TSX |
| [`type.contract`](references/checkers/type.contract.md) | Aggregated nullable declarations, bare nullish fallbacks and oversized unions; one switch for two diagnostics | TypeScript, TSX |
| [`type.annotation`](references/checkers/type.annotation.md) | Broad or bare Python annotations and invalid forward annotations | Python |
| [`type.signature`](references/checkers/type.signature.md) | Missing Python parameter and return annotations | Python |
| [`type.broad`](references/checkers/type.broad.md) | Recognized broad type markers and JSON serialization outside proven outputs and permitted library inputs | All; markers differ per language |
| [`type.boundary`](references/checkers/type.boundary.md) | Recognized library results and callback inputs used without typed conversion | All |
| [`unused.parameter`](references/checkers/unused.parameter.md) | Parameters without resolved read usage | All |
| [`unused.function`](references/checkers/unused.function.md) | Non-exported functions and methods without resolved references | All |
| [`unused.field`](references/checkers/unused.field.md) | Fields without reads; initialization and writes alone do not count | All |
| [`unused.return`](references/checkers/unused.return.md) | Known non-void results discarded at a call site; always a warning | All |
| [`type.assertion`](references/checkers/type.assertion.md) | Unchecked casts and assertions | Python, Java, TypeScript, TSX |
| [`type.suppression`](references/checkers/type.suppression.md) | Foreign suppression directives and malformed or unused next-line exceptions | All |
| [`imports.wildcard`](references/checkers/imports.wildcard.md) | Python wildcard imports | Python |
| [`architecture.owner`](references/checkers/architecture.owner.md) | Missing or overlapping responsibility owners | All |
| [`architecture.structure`](references/checkers/architecture.structure.md) | Selected modules outside their configured owning locations | All |
| [`architecture.import`](references/checkers/architecture.import.md) | Dependencies forbidden by configured boundaries | All |
| [`architecture.dispatch`](references/checkers/architecture.dispatch.md) | Language and rule identity comparison or match/switch dispatch | All |
| [`architecture.object_model`](references/checkers/architecture.object_model.md) | Non-underscore module-level functions in configured extension paths; Go reports unexported lowercase package-level functions too, receiver methods and Java are not reported | Python, Go, JavaScript, TypeScript, TSX |

Every analyzer registers the same base set of 19 diagnostic IDs:
`unused.parameter`, `unused.function`, `unused.field`, `unused.return`,
`type.boundary`, `type.suppression`, `size.lines`, `size.statements`,
`size.complexity`, `size.nesting`, `size.parameters`, `architecture.owner`,
`architecture.structure`, `architecture.import`, `architecture.dispatch`,
`architecture.object_model`, `structure.guarded_projection`, `test.tautology`
and `test.vacuous`.

`test.deletion` is not part of that per-language set: no analyzer registers it,
and it is produced by the run-level deletion gate only in `--base` and `--staged`
mode.

The remaining nine diagnostic IDs are registered per language, and a registered
diagnostic can only report for the analyzer that registered it:

| Analyzer | Additional diagnostic IDs |
|---|---|
| Python | `type.annotation`, `type.signature`, `type.assertion`, `type.broad`, `imports.wildcard`, `flow.swallow` |
| Go | `type.broad` |
| Java | `type.assertion`, `type.broad`, `flow.swallow` |
| JavaScript and JSX | `type.broad`, `flow.swallow`, `flow.fallback` |
| TypeScript and TSX | `type.assertion`, `type.broad`, `type.nullable`, `type.union`, `flow.swallow`, `flow.fallback` |

Key differences:

- `type.annotation`, `type.signature` and `imports.wildcard` appear in Python
  files only.
- `type.assertion` covers Python, Java, TypeScript and TSX, but not Go or
  JavaScript.
- `type.nullable` and `type.union` appear in TypeScript and TSX files only.
- `flow.swallow` covers Python, Java and the JavaScript family; `flow.fallback`
  appears in JavaScript, JSX, TypeScript and TSX files only.
- `unused.return` is always a warning, snapshot-only `test.vacuous` findings are
  warnings, and `test.deletion` reports a changed snapshot path as a warning;
  every other diagnostic reports errors. Warnings never change the exit code.
- `architecture.object_model` is registered by every analyzer, but only
  grammars with module-level functions can report. Go is included: a
  package-level function whose name does not start with `_` is reported, so an
  unexported `func check(...)` is a finding — the test is the leading underscore,
  not export status. Go receiver methods and Java files produce no findings.
- `type.broad` markers differ per language: `typing.Any`,
  `typing_extensions.Any` and `pydantic.JsonValue` references plus internal
  `json.dumps` results in Python; `any`, empty interfaces and imported
  `encoding/json` results in Go; `java.lang.Object`, unbounded wildcards and
  recognized raw collections plus Gson/Jackson results in Java; `JSON.stringify`
  results in JavaScript and JSX; `any`, `unknown`, `object` and empty object
  types plus `JSON.stringify` results in TypeScript and TSX.
- One Python annotation can report twice: `typing.Any` triggers both
  `type.annotation` and `type.broad`.

Coverage does not promise complete evidence for every construct. The resolution
limits for boundaries, usage, encodings and guarded projections are recorded on
the corresponding checker pages and remain review obligations.

Full detail for every checker — role, language differences, options, defaults,
types and correct/incorrect configuration — is on its own page linked from the
[checker index](references/checkers.md).

## Checker configuration

Each repository commits its policy at `.pluvy/pluvy.json`; the gate
never changes it. The policy separates global file selection from checker
options:

```json
{
  "version": 2,
  "include": ["src/*", "tests/*"],
  "exclude": [],
  "third_party_modules": [],
  "checkers": {
    "function_size": {"lines": 40},
    "architecture.owner": {
      "owners": [{"name": "domain", "paths": ["src/domain/*"]}]
    }
  }
}
```

Rules that apply to every checker:

- `enabled` is a strict boolean, defaulting to `true`. JSON `true` or `false`
  only: `"false"`, `0` and `null` are errors. Omit `enabled: true` and entries
  that only repeat defaults.
- Unknown checker IDs and unknown options are errors, including when the checker
  is disabled. The same rejection applies inside nested option objects.
- `function_size` is one checker with one switch: it owns `size.lines`,
  `size.statements`, `size.complexity`, `size.nesting` and `size.parameters`.
  Suppression comments name those diagnostic IDs, not the checker key.
- `include` and `exclude` are the only global file selectors and must stay at the
  top level. Architecture `placements`, `boundaries` and `paths` select where
  that one architecture check applies, not which files other checkers read.
  `owners` instead declares the required responsibility mapping: when it is
  configured and the checker is enabled, every selected file must match exactly
  one owner.
- `third_party_modules` is a top-level list of exact import specifiers. It grants
  a directional library-input exception for erased dictionaries, not a file or
  checker exemption.
- `type.broad.encoding` holds four lists, all empty by default: `encoders`,
  `decoders`, `outputs` and `instance_outputs`. They extend built-in
  serialization recognition; they never exempt broad annotations, casts or
  incoming library results.

```json
{
  "version": 2,
  "third_party_modules": ["vendor.client"],
  "checkers": {
    "structure.guarded_projection": {"min_fields": 4},
    "type.broad": {
      "encoding": {
        "encoders": ["vendor.client.encode"],
        "outputs": [{"call": "vendor.client.publish", "arguments": [0]}]
      }
    }
  }
}
```

An invalid policy, rejected before any file is checked:

```json
{
  "version": 2,
  "checkers": {
    "function_size": {"enabled": "false"},
    "type.broad": {"maximum": 3},
    "syntax.invalid": {"enabled": false}
  }
}
```

The string flag fails the strict boolean, `maximum` is not a `type.broad`
option, and `syntax.invalid` is not a checker. Malformed configuration fails
closed with exit code 2.

Thresholds, excludes, owners, boundaries and enable flags are architecture
decisions. Changing any of them requires a separate full audit and review, and a
staged policy change stays blocked until that audit passes and the change is
committed. Disabling a checker or raising a limit is never remediation. The full
option list, defaults, types and examples for each checker are on the page
linked from the [checker index](references/checkers.md).

## Pointing other agents at this skill

For agents that take their instructions from a repository instruction file
(such as AGENTS.md), paste this pointer into that file (manual paste only; do
not auto-rewrite a user's instruction file):

```markdown
<!-- pluvy skill pointer -->
When adopting, configuring, or running pluvy, or triaging its findings,
first read `.agents/skills/pluvy/SKILL.md` and follow it.
```

## References

- [Agent integration notes](references/agent-integration.md) — integration
  paths for any agent and the limits that apply.
- [Checker index](references/checkers.md) — the 22 checkers and the 29
  diagnostic IDs, with coverage tables and one page per checker for its role,
  language differences, options, defaults, types and configuration examples.
- [Approved next-line diagnostic exceptions](references/comment-suppressions.md)
  — the suppression directive syntax, validation rules and their snapshot scope.
- [Policy sample](references/policy-sample.md) — a compact policy covering file
  selection, owners, import boundaries and function-size thresholds.
