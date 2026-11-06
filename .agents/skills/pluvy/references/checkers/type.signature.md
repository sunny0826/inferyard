# `type.signature`

[Back to the checker index](../checkers.md)

Reports Python functions whose parameters or return value have no annotation.

**Severity**: error.

**Languages**: Python only.

Untyped parameters are listed by name, and a missing return annotation is listed
as `return`. `self` and `cls` do not need annotations and are not listed; untyped
`*args` and `**kwargs` names are. Lambdas are not covered, and functions whose
syntax could not be recovered are skipped rather than guessed.

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "type.signature": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "type.signature": {"require_return": true}
  }
}
```

Disabling this checker is a policy change and does not authorize untyped
interfaces.
