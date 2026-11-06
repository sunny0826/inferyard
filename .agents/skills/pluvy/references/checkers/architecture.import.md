# `architecture.import`

[Back to the checker index](../checkers.md)

Reports dependencies forbidden by the configured boundaries.

**Severity**: error.

**Languages**: every analyzer; selectors use language-appropriate names.

| Language | Selector rules |
| --- | --- |
| Python and Go | Python and Go boundaries use module-name patterns, and Python relative imports are normalized using their source directory. |
| JavaScript and TypeScript | JavaScript and TypeScript boundaries use module specifiers or normalized relative path patterns, with and without source extensions, and relative imports also match root-relative paths. |
| Java | Java imports use qualified type and member names, including static imports and wildcard suffixes. |

Type-only imports, re-exports, literal `require()` and literal `import()` are
checked. Include both a package itself and its descendants where needed. All
matching boundaries apply. Dynamic imports, escaped module strings and build
aliases still need language-aware review.

**Options**: `enabled` (boolean, default `true`); `boundaries` (list, default
empty). Each boundary is `{"name": ..., "sources": [...], "forbidden_imports":
[...]}` with a non-empty name and at least one source and one forbidden pattern.
Ownership does not infer dependency permissions, and an empty boundary list
provides no project-specific facts to enforce.

Correct:

```json
{
  "checkers": {
    "architecture.import": {
      "boundaries": [
        {
          "name": "domain does not depend on adapters",
          "sources": ["src/domain/*"],
          "forbidden_imports": ["adapters", "adapters.*"]
        }
      ]
    }
  }
}
```

Incorrect — a boundary without forbidden imports:

```json
{
  "checkers": {
    "architecture.import": {
      "boundaries": [{"name": "domain", "sources": ["src/domain/*"]}]
    }
  }
}
```
