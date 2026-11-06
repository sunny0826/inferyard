# `unused.parameter`

[Back to the checker index](../checkers.md)

Reports declared parameters without resolved read usage.

**Severity**: error.

**Languages**: every analyzer.

Python `self`/`cls`, JavaScript `this` and blank `_` bindings are not candidates.
Positional arguments match by position, keyword-only arguments by name, and
variadic slots separately. Methods linked by resolved inheritance share
parameter-use evidence: a read in any implementation makes the corresponding
parameter used throughout its override family, including base and sibling
implementations and imported bases; extra arguments and parameters unread
throughout the family remain findings, and unrelated same-named methods do not
gain the exemption. Structural interface relationships without resolved
inheritance still need review. Required signature constraints should be explained
rather than fixed by deleting a parameter.

#### TypeScript

TypeScript constructor parameters marked `public`, `protected`, `private` or
`readonly` can declare instance properties, and resolved reads of those
properties count as usage of the exact constructor parameter declaration.
Initialization and writes alone remain insufficient, and ordinary parameters or
unrelated declarations with the same name remain reportable.

#### Java

For Java, resolved `extends` and `implements` contracts link an override family,
and parameter matching stays name/slot based without proving Java signature
compatibility. Explicit unused parameters of public, protected and
package-visible methods, annotated entry points and constructors remain checked
when a body exists. Compact-constructor parameters are not synthesized into
semantic declarations.

## Configuration

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "unused.parameter": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "unused.parameter": {"ignore_prefix": "_"}
  }
}
```
