# `unused.return`

[Back to the checker index](../checkers.md)

Warns when a known non-void return value is discarded.

**Severity**: always a warning. Warnings are labelled in text reports and remain
in JSON `findings`, and they never change the exit code: a run with only these
warnings exits 0. Treat them as review evidence, not a failing gate.

**Languages**: every analyzer.

A known non-void local or resolved imported function result discarded as a
standalone call, a Python or Go `_` assignment or a JavaScript `void` expression
produces a warning. Unknown external return types are not guessed. Promises,
coroutines, generators and containers remain values even when their payload is
`void` or `None`; Java `Void` and Go empty structs also remain values. TypeScript
and TSX direct generic results, Python imported `Callable[..., R]` contracts and
JavaScript forwarding of an unchanged callback parameter can prove a result; a
callback's declared contract takes precedence over its body.

| Language | Discarded-return rules |
| --- | --- |
| Java | A Java overload family that mixes void and non-void members does not produce a discarded-return warning. |

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "unused.return": {"enabled": false}
  }
}
```

Incorrect — unknown option, and no severity override exists:

```json
{
  "checkers": {
    "unused.return": {"severity": "error"}
  }
}
```
