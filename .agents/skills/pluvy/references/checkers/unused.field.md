# `unused.field`

[Back to the checker index](../checkers.md)

Reports fields without read usage.

**Severity**: error.

**Languages**: every analyzer.

A field needs an actual read: declaration, initialization and subsequent writes
alone do not make it used. Reads associate with the actual declaring type and
instance, so a same-named field on another class is not evidence. Objects passed
to configured third-party libraries, including objects returned by a registered
callback, have all their declared fields treated as consumed; this is not
inferred for unrelated objects or for values arriving from the library. A class
that inherits from a configured library base, such as a `pydantic` model, has its
declared fields treated as consumed rather than reported as write-only.

| Language | Field rules |
| --- | --- |
| Java | Java implicit record fields and accessors are not synthesized into semantic declarations. |

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "unused.field": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "unused.field": {"count_writes": false}
  }
}
```
