# `type.suppression`

[Back to the checker index](../checkers.md)

Reports suppression directives that the policy does not permit, and malformed or
unused approved next-line exceptions.

**Severity**: error.

**Languages**: every analyzer.

Shared foreign directives are reported in every supported language: `noqa`,
`type: ignore`, `pyright: ignore`, `mypy: ignore`, the `@ts-ignore` family, the
`eslint-disable` family, `nolint` and `lint:ignore`. Two further marker families
are language-specific:

| Language | Markers |
| --- | --- |
| Java | `NOSONAR`, `CHECKSTYLE:OFF`, `noinspection` and the `@SuppressWarnings`/`@SuppressFBWarnings` annotations. |

The only allowed form is a standalone next-line comment using the language's own
line marker (`#` in Python, `//` in Go, Java and the JavaScript family) naming one
exact diagnostic ID and a nonblank reason, for an exception that received
explicit human approval beforehand. Block comments, trailing directives,
file-wide forms, wildcards, lists, missing reasons, unknown IDs, the
`type.suppression` ID itself and a directive whose target line has no matching
diagnostic are all reported. Verified comment nodes only are considered; strings
and template text cannot suppress anything.

#### Python

references to the checking-disabling markers `typing.no_type_check` and
`typing.no_type_check_decorator`, in any reference form. That includes
`@no_type_check`, `@no_type_check_decorator` and a `no_type_check(...)` call such
as `typing.no_type_check(1)`; import aliases resolve to the same identity, so
`from typing import no_type_check` followed by `@no_type_check` is reported too.
Only the `no_type_check(...)` call form additionally reports `type.assertion`.

## Configuration

**Options**: None beyond `enabled` (boolean, default `true`).

Correct — disabling reports nothing, and no approval file or hash is involved:

```json
{
  "checkers": {
    "type.suppression": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "type.suppression": {"allow_reasons": []}
  }
}
```
