# `unused.function`

[Back to the checker index](../checkers.md)

Reports non-exported functions and methods without resolved references.

**Severity**: error.

**Languages**: every analyzer.

Declarations that keep an external-use contract are not reported as dead:

| Language | External-use contract |
| --- | --- |
| Python | public module APIs, plus recognized runtime entry points and declaration-only signatures. |
| Go, JavaScript, TypeScript and TSX | exported functions, plus recognized runtime entry points and declaration-only signatures. |
| Java | declarations that carry no modifier block, carry an annotation, are overloaded (more than one method with the same name in the enclosing type) or are not `private`, constructors included. Only a private, unannotated, unoverloaded declaration with no resolved reference is a candidate. |

Callers, closures and literal imports resolve through the selected snapshot view.
An unresolved member read or protocol dispatch defers same-named member candidates
instead of asserting they are unused. Absence of a local reference does not prove
a repository-wide unused declaration; review candidates before changing code.

#### Java

Java method references count as resolved references, and project evidence resolves
explicit class imports, same-directory package peers and `extends`/`implements`
contracts.

## Configuration

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "unused.function": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "unused.function": {"include_private": true}
  }
}
```
