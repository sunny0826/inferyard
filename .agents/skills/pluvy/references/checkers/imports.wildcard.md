# `imports.wildcard`

[Back to the checker index](../checkers.md)

Reports Python wildcard imports.

**Severity**: error.

**Languages**: Python only.

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "imports.wildcard": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "imports.wildcard": {"allow": ["typing"]}
  }
}
```
