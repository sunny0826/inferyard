# Agent integration notes — pluvy skill

How a consuming repository connects an agent to this skill, and the limits
that apply. Anchors in these notes point inside the skill directory; bare file
names such as `README.md` name files of the pluvy source repository for
reference only, not resolvable anchors; no network location is assumed.

## Generic statement (any agent)

This skill is a plain directory with a `SKILL.md` in Agent Skills format. Any
agent that implements the Agent Skills directory convention can discover it
under `.agents/skills/` (project level, found from the working directory up
through its ancestors) or `~/.agents/skills/` (user level). Any agent that can
read files and run shell commands can also use it without a discovery
mechanism: read `.agents/skills/pluvy/SKILL.md` from the consuming
repository and follow it, for example through the AGENTS.md pointer snippet in
SKILL.md. Nothing here promises that a specific tool will discover or load the
skill on its own; the concrete integration steps are up to each agent.

## Repository adoption and automatic runs

Skill discovery is separate from repository adoption. Installing this skill at
user level makes it available across repositories; it does not require every
repository to run the gate. Follow [the applicability decision](../SKILL.md#decide-whether-the-gate-applies)
before automatic execution or reporting. A pointer to this skill is a reading
instruction, not a project requirement to run the gate.

| Repository or task context | Expected agent behavior |
|---|---|
| Global executable or skill only; no project policy, requirement or explicit run request | No gate command and no gate notice in the task report |
| Default project policy exists | Run the post-task gate and report its actual result |
| Project instructions, scripts or CI explicitly require the gate via a custom `--config` | Use the declared root and config, even without the default policy |
| Project requires the gate but its policy is missing | Report the configuration failure; do not treat it as non-adoption |
| Policy is malformed or unreadable | Report the failure; preserve exit code 2 |
| User explicitly requests a run without a policy | Attempt the requested run and report the configuration failure |
| User asks about capabilities before adoption | Answer the question without automatically running the gate or creating a policy |

Do not create a policy to silence a post-task warning. Adoption is a separate
requested task. Repository tests, linting and type checks retain their own
requirements regardless of whether this gate applies.

## Adapting agent-specific skill directories

Some agents load skills only from their own directory. Where that directory
lives decides the form:

- Outside the audited repository root (`--root`), for example a user-level or
  agent-specific skills directory, a symlink keeps a single source of truth:

```sh
ln -s /absolute/path/to/repo/.agents/skills/pluvy <agent-skills-dir>/pluvy
```

- Inside the repository root, copy the directory instead; never adapt with a
  directory symlink there. The full audit `--all` refuses to traverse any
  directory symlink under the root and exits 2, and the refusal happens
  during enumeration, before include/exclude filtering — no exclude pattern
  bypasses it. The Git-based modes do not enumerate the disk, which hides the
  damage: on an unchanged tree `pluvy --base HEAD` still exits 0 with
  a 0-files report while every full audit (bootstrap, policy re-review, and
  the D12 fallback row) is permanently broken; a `--base` run with changed
  files exits 2 as soon as usage evidence is read from disk. Treat the copy
  as a drifting duplicate: re-copy after every update of the source,
  otherwise the two locations diverge silently. Windows systems without
  usable symlinks use the copy form everywhere.

## Team / CI usage

Any CI system that can run the gate container image integrates with the same
commands used locally. The pluvy repository itself runs the same
engineering checks in GitHub Actions (`.github/workflows/engineering.yml`).
A pre-commit hook can run `uv run --no-sync pluvy --staged` once the
policy file is committed; the hook must be installed explicitly, because the
tool never changes Git configuration. The `README.md` integration section
carries the background.
