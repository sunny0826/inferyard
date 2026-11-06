# `architecture.structure`

[Back to the checker index](../checkers.md)

Reports selected source modules that fall outside their configured owning
locations.

**Severity**: error.

**Languages**: every analyzer; the check is structural and language-independent.

A file matching a placement's `paths` must also match at least one of that
placement's `allowed_paths`. Each matching placement is enforced independently,
and an unrelated placement does not affect the file. Findings name the constraint
and list the allowed locations. The check never moves files, and it does not infer
a file's domain from its name.

**Options**: `enabled` (boolean, default `true`); `placements` (list, default
empty). Each placement is `{"name": ..., "paths": [...], "allowed_paths": [...]}`
with a non-empty name, at least one selector path and at least one allowed path.

Correct:

```json
{
  "checkers": {
    "architecture.structure": {
      "placements": [
        {
          "name": "language rule modules belong with language plugins",
          "paths": ["src/*_rules.py"],
          "allowed_paths": ["src/languages/*"]
        }
      ]
    }
  }
}
```

Incorrect — a placement without `allowed_paths`:

```json
{
  "checkers": {
    "architecture.structure": {
      "placements": [{"name": "rules", "paths": ["src/*_rules.py"]}]
    }
  }
}
```
