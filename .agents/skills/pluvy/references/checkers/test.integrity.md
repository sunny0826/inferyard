# `test.integrity`

[Back to the checker index](../checkers.md)

Reports assertions that cannot fail (`test.tautology`) and tests without a
meaningful assertion (`test.vacuous`). Only the current syntax tree is inspected:
the checker does not execute tests, follow data flow or compare snapshot content.

**Severity**: error for `test.tautology` and for `test.vacuous`, except that a
test whose only assertions are snapshot matchers reports `test.vacuous` as a
warning.

**Languages**: every analyzer; recognized test declarations, assertions and
control-flow statements differ:

| Language | Test declarations and recognized assertions | Control-flow, context-manager and abort statements |
| --- | --- | --- |
| Python | Module-level and class-level `def test_*` functions, excluding a function whose decorator base name is `fixture` (`@pytest.fixture`, `@pytest.fixture(...)` or `@fixture`); `assert` statements and `assert*`/`_assert*` calls, with `pytest.raises` and `assertRaises` counted as assertions. Tautology detection covers `assert` statements and the positive-equality calls `assertEqual`, `assertEquals` and `assertIs`. | `with`/`async with`, `for`, `while`, `if`, `try`, `match` and `raise`. |
| JavaScript, TypeScript, TSX | `it(...)` and `test(...)` callbacks; `expect(...).matcher(...)` chains and `assert*`/`_assert*` calls. Tautology detection covers `expect(...).toBe`/`toEqual`/`toStrictEqual`, single-argument literal-`true` assertions, and `assert`/`_assert` members `equal`, `strictEqual`, `deepEqual` and `deepStrictEqual`. | `if`, `for`, `for...in`/`for...of`, `while`, `do`, `try`, `switch`, `with` and `throw`. |
| Go | `func Test*` functions; `t.Error*`, `t.Fatal*` and `t.Fail*` calls. | `if`, `for`, `switch` and `select`. |
| Java | `@Test` methods; `Assertions.assert*` and inherited `assert*` calls plus `assert` statements. Tautology detection covers literal-`true` and `==`-mirrored `assert` statements, `assertTrue(true)`, and the positive-equality calls `assertEquals` and `assertSame`. | `if`, `for`, enhanced `for`, `while`, `do`, `try`, try-with-resources, `switch`, `synchronized` and `throw`. |

A test is reported by default only when its path matches one of the default test
globs. The checker classifies recognized assertions one site at a time. A
recognized assertion is a tautology only in these frozen positive forms: a
literal-`true` assertion; a single-argument literal-`true` call whose name is not
negated; an `assert` statement whose operands mirror on `==`/`is`, or an equality
matcher whose expected and actual operands mirror; or a named positive-equality
call from the per-language set in the table above. Negative and non-equality
forms such as `assertNotEqual`, `assertLess`, `assertFalse` and `assert.notEqual`
stay outside the set. The always-true matchers are `.not.toThrow()`,
`toBeTruthy()` and `toBeDefined()`. Go does not claim mirrored or always-true
forms. Each assertion site produces at most one diagnostic, and a test containing
both kinds reports each defective site separately.

A test with no recognized assertion is vacuous only when its body also has no
unrecognized call and no control-flow, context-manager or abort statement listed
in the table. This is the conservative confirmed-no-assertion standard: only a
body made of literals, assignments and print calls is reported. Calls to the language
print family are exempt from that conservative check: Python `print`,
JavaScript-family `console.*`, Go `fmt.Print*`/`log.Print*` and Java
`System.out.print*`/`System.err.print*`. Any other unrecognized call can be a
custom assertion helper, so the missing-assertion signal stays silent rather
than guessing that the helper cannot fail. A constructor invocation (`new` in
JavaScript, object creation in Java) is handled the same way: it is never an
assertion and is never classified as mirrored or always-true, and the checker
does not infer constructor behavior. Comments inside an argument list are
ignored and do not change argument counts. Assertions and calls anywhere inside
the body, including uncalled nested functions and closures, count for the outer
test. The snapshot signal is independent: a test whose recognized assertions are
all `toMatchSnapshot()` or `toMatchInlineSnapshot()` reports one warning at the
first snapshot assertion, because setup calls do not change the fact that the
test verifies no behavior.

A finding can be suppressed on the next line by naming `test.tautology` or
`test.vacuous` in an approved directive with a reason; the directive applies to
both diagnostics independently. A missing-assertion or snapshot finding can also
be avoided by removing the test from the configured test paths. Disabling
`test.integrity` disables both diagnostics, and text reports list the switch name
rather than the diagnostic IDs.

**Options**: `enabled` (boolean, default `true`); `paths` (tuple of strings,
default `("test_*.py", "*/test_*.py", "*_test.py", "tests/*", "*/tests/*",
"*.test.js", "*.test.jsx", "*.test.ts", "*.test.tsx", "*.test.mjs", "*.test.cjs",
"*.test.mts", "*.test.cts", "*.spec.js", "*.spec.jsx", "*.spec.ts",
"*.spec.tsx", "*_test.go", "*Test.java", "*Tests.java")`). Paths use the shared
case-sensitive `fnmatchcase` dialect: `*` spans `/`, and `**` or brace
alternation have no special meaning. An empty tuple selects no files.

Correct — restrict the checker to two test layouts:

```json
{
  "checkers": {
    "test.integrity": {"paths": ["tests/*", "*.spec.ts"]}
  }
}
```

Incorrect — `globs` is not an option and unknown options are rejected:

```json
{
  "checkers": {
    "test.integrity": {"globs": ["tests/*"]}
  }
}
```

Known limits are deliberate. A custom assertion helper is recognized only when
its name starts with `assert` or `_assert`; other helpers suppress the
missing-assertion signal but are never proven to assert. Negative and inequality
assertions (`assertNotEqual`, `assertLess`, `assert.notStrictEqual`) are not
reported as tautologies: they fail rather than pass, which is a different defect
outside this checker. `assertFalse(false)` is not detected. A custom equality
helper outside the per-language set is not a tautology, although an
`assert*`/`_assert*` name still counts as an assertion for the conservative
missing-assertion check. In JavaScript, a bare identifier call such as
`assertSame(x, x)` is not mirrored; only members of an `assert`/`_assert` root
use the equality set. Identical spelling is not data flow, so
`assert result == service` is not a tautology even when both
names refer to the same value, and a call versus a literal is not a tautology.
JavaScript declaration recognition covers only `it(...)` and `test(...)`, so
`xit`, `xtest`, `it.only`, `test.skip` and `it.each` variants are not test
declarations and their assertions are missed. Go recognizes assertion calls by
member name without validating the receiver, so `err.Error()` counts as an
assertion. These recognition limits also affect the baseline declaration names
and assertion counts used by [`test.deletion`](test.deletion.md).
Snapshot-only tests receive a warning rather than an error. The path globs can
include non-test files with test-like names, so tighten `paths` when the default
set does not describe a repository.
