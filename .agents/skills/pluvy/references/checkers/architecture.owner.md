# `architecture.owner`

[Back to the checker index](../checkers.md)

Reports files whose responsibility ownership is missing or overlapping.

**Severity**: error.

**Languages**: every analyzer; the check is structural and language-independent.

When owners are configured and the checker is enabled, every selected file must
match exactly one owner. Ownership does not infer dependency permissions, and an
empty owner list provides no project-specific facts for the check to enforce.

**How owners behave**: `owners` is a required mapping, not a scope selector. It
does not choose which files the check looks at; all selected files are checked,
and a file that matches no owner or more than one owner is reported. Use
`include`/`exclude` to drop files, and `architecture.structure` placements or
`architecture.object_model` paths to scope those other checks.

**Options**: `enabled` (boolean, default `true`); `owners` (list, default empty).
Each owner is `{"name": ..., "paths": [...]}` with a non-empty name and a
non-empty `paths` list. Each pattern is a case-sensitive shell pattern where `*`
spans `/`, and paths are relative to the source root. Validation requires at
least one path entry; it does not require each entry to be non-empty, so an empty
string pattern is accepted by the configuration and matches no file, which
contributes no ownership.

Correct:

```json
{
  "checkers": {
    "architecture.owner": {
      "owners": [
        {"name": "domain", "paths": ["src/domain/*"]},
        {"name": "tests", "paths": ["tests/*"]}
      ]
    }
  }
}
```

Incorrect — an owner without a `paths` list, and a boundaries list misplaced under
`architecture.owner`:

```json
{
  "checkers": {
    "architecture.owner": {
      "owners": [{"name": "domain"}],
      "boundaries": []
    }
  }
}
```
