# `architecture.dispatch`

[Back to the checker index](../checkers.md)

Reports language and rule identity dispatch: comparisons of selectors named
`language` or `rule_id` with literal identities or uppercase member constants,
and `match`/`switch` statements on those selectors.

**Severity**: error.

**Languages**: every analyzer, using each language's own comparison and switch
syntax. One Java difference:

| Language | Difference |
| --- | --- |
| Java | additionally recognizes `switch` expressions and `equals` comparisons on those selectors, with enum constants. |

Comparing two runtime identities for compatibility is allowed. The check cannot
detect every alias or indirect dispatch, so cohesion and interface conformance
remain review obligations.

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "architecture.dispatch": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "architecture.dispatch": {"selectors": ["language"]}
  }
}
```
