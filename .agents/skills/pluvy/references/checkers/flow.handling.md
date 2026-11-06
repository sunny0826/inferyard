# `flow.handling`

[Back to the checker index](../checkers.md)

Reports wide exception captures that swallow failures (`flow.swallow`) and
coalescing chains that hide missing data behind literal defaults
(`flow.fallback`). Only the current syntax tree is inspected: the checker does
not execute handlers, follow data flow or judge whether a default value is
semantically appropriate.

**Severity**: error.

**Languages**: Python, Java, JavaScript, JSX, TypeScript and TSX; the recognized
wide captures differ:

| Language | Wide captures |
| --- | --- |
| Python | A bare `except:` or a declared `Exception`/`BaseException` alternative, including a tuple alternative. |
| Java | A `catch` whose single or `\|`-separated `catch_type` includes `Exception` or `Throwable`. |
| JavaScript, JSX, TypeScript, TSX | Any `catch` clause, or a `.catch(...)` call with an inline function callback. |

A capture is reported only when its handler subtree contains no re-throw. The
whole subtree counts, including nested functions, so a nested function that
re-throws suppresses the finding even when that function never runs. A handler
that only logs, prints or returns a substitute value remains a finding. A
finding is anchored at the captured handler clause (`except` or `catch`) or at
the start of the swallowing call expression; a multi-line chained call reports
the line where the whole call begins, not the `.catch` token line.

#### JavaScript, JSX, TypeScript and TSX

For `.catch(...)`, only the first argument is the rejection handler. When that
argument is not an inline function, such as an identifier naming a handler or
`null`, the call is not reported because the checker cannot inspect its body;
later arguments are ignored. A `.catch(() => constant)` reports `flow.swallow`
and never `flow.fallback`.

A coalescing chain is the maximal set of `??` expressions connected through
parenthesized expressions, assertions, member access on the object side, and
call receivers and arguments. Optional chains participate through the same
member access. Any other operator, including logical, arithmetic, assignment,
ternary and spread forms, breaks the chain, and an arrow or function body starts
an independent chain for the `??` expressions inside it. The default of one
`??` is its direct right operand. A default is a bare literal only when it is a
number, string, template string, object, array or boolean literal; a
parenthesized, asserted or called default is not a literal, and `null` or
`undefined` belongs to `type.nullable` instead. A chain is reported only when
its operator count exceeds `max_chain` and every default is a bare literal, so a
mixed chain stays silent. The finding is anchored at the deepest `??` (the chain
head), with the earliest source position breaking a tie. The `||` operator is
not a coalescing chain and is never reported.

## Configuration

**Options**: `enabled` (boolean, default `true`); `max_chain` (integer, at least
1, default 3) limits the number of `??` operators in one chain. The threshold is
strict: a chain with exactly `max_chain` operators passes.

Correct — require a longer chain before reporting:

```json
{
  "checkers": {
    "flow.handling": {"max_chain": 4}
  }
}
```

Incorrect — the limit must be positive and `flow.fallback` is not a checker key:

```json
{
  "checkers": {
    "flow.handling": {"max_chain": 0},
    "flow.fallback": {"enabled": false}
  }
}
```

Known limits are deliberate. The checker does not prove that a captured error
should terminate the program, that a fallback changes business meaning, or that
a literal is the wrong default; review the owning contract and its consumers.
A fully qualified catch type such as `java.lang.Exception` or a Python dotted
capture type such as `module.Exception` is not recognized because the spelling
is compared literally, and a computed member reference such
as `promise["catch"]` is not recognized while the optional call
`promise.catch?.(...)` is; both are conservative misses. Go has no exception
handler and is not covered: an `err` value discarded or logged without
propagation is a different failure shape. An approved next-line directive can
name `flow.swallow` or `flow.fallback` independently; disabling `flow.handling`
disables both diagnostics.
