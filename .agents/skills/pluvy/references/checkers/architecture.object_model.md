# `architecture.object_model`

[Back to the checker index](../checkers.md)

Reports non-underscore module-level functions in the configured extension paths,
where behavior must belong to objects.

**Severity**: error.

**Languages**: every analyzer registers the check, but only grammars that have
module-level functions can produce findings. Candidates differ by language:

| Language | Candidates |
| --- | --- |
| Python, JavaScript, TypeScript and TSX | module-level function declarations, and JavaScript/TypeScript/TSX function-valued declarations, whose name does not start with `_`. A lowercase name such as `check` is reported exactly like `Check`; no export keyword is required. |
| Java | no findings. Java has no module-level functions; its methods belong to classes, which are the object seam this check asks for. |

The candidate test is the leading underscore of the declared name, not export
status or capitalization.

Per-checker enable flags are policy controls; there is no path-scoped exception
inside one file.

#### Go

package-level functions whose name does not start with `_`. An unexported
lowercase function such as `func check` is reported too: the test is the leading
underscore, not export status, so this is not an "exported functions only" rule.
Receiver methods such as `func (r Rule) Check` are methods, not module-level
functions, and are not reported.

## Configuration

**Options**: `enabled` (boolean, default `true`); `paths` (list of path patterns,
default empty). An empty list provides no project-specific facts to enforce.
Patterns use the same case-sensitive shell matching as other policy paths.

Correct:

```json
{
  "checkers": {
    "architecture.object_model": {
      "paths": ["src/languages/*/rules.py"]
    }
  }
}
```

Incorrect — `class_modules` is not a valid key; the same selectors belong under
`paths`:

```json
{
  "checkers": {
    "architecture.object_model": {"class_modules": ["src/languages/*"]}
  }
}
```
