# `test.deletion`

[Back to the checker index](../checkers.md)

Reports removed test files, assertion erosion in surviving changed test files, and
changed snapshot paths when a changed audit runs.

**Severity**: error for a removed test file or assertion erosion, and warning for a
changed snapshot path.

**Languages**: all supported languages.

The gate is a run-level check rather than an analyzer rule. It runs only for
`--base` and `--staged` audits; `--all` never activates it. In a changed audit,
the gate compares the baseline commit with the current snapshot. A file that the
comparison reports as deleted, subject to the configured test paths, reports one
`test.deletion` error at line 1, column 1 with `scope: "file"`. An exact rename
that Git detects with 100% similarity is excluded; a partial rename appears as a
new file plus a deletion, so its removed path is still reported. The message
includes the baseline test-declaration and assertion counts when the baseline can
be parsed.

A surviving changed test file is compared with its own baseline. Test declaration
names are compared as a multiset, so removing one of two same-named declarations
is visible while reordering declarations is not. Assertion counts are compared
with the same declaration and assertion recognition used by `test.integrity`;
nested function bodies count toward their outer test. The checker reports one
error per file when a baseline declaration name is missing or when the assertion
count fell by more than `removed_assertions`. Pure reordering and renaming of
helpers therefore stay silent, but renaming a test declaration itself changes the
multiset and is reported. A parser rejection of either tree skips the erosion
signal for that file, while a deleted file is still reported even when its
baseline syntax is rejected.

A changed path matching `snapshot_paths` reports one warning per path. Addition,
modification, deletion and both sides of a non-exact rename are included, and in
`--base` mode an untracked snapshot file is included too. Snapshot content is not
compared. Global `include` and `exclude` do not filter this path signal; tighten
`snapshot_paths` or disable the checker instead.

The gate does not use next-line suppression: a deleted file has no current
snapshot, and an erosion finding has no corresponding current defect node. A
`pluvy: ignore-next-line test.deletion` directive in a surviving file is therefore
an invalid directive and produces `type.suppression`. Disable the checker with
`enabled: false` or tighten `paths`/`snapshot_paths` when a reduction is
intentional. Deleted files are not analyzed inputs, so `Report.files` counts only
the selected current sources; deletion findings are added from baseline evidence.
An unmerged index in `--staged` mode and a baseline blob that is not valid UTF-8
both fail closed with exit code 2.

Declaration and assertion recognition belongs to
[`test.integrity`](test.integrity.md); this page does not duplicate it. The known
false-positive surface follows from that comparison: legitimately deleting an
obsolete test, or renaming a test declaration, is reported because no similarity
or intent analysis exists. A deleted helper file that matches the test globs, such
as a `tests/` support module, is reported with zero baseline declarations and zero
assertions for the same reason. Tighten the test globs or disable the checker for
such a deliberate change.

Correct diff — a deliberate obsolete-test removal scopes the checker away in the
same change:

```diff
--- a/.pluvy/pluvy.json
+++ b/.pluvy/pluvy.json
@@
-    "test.deletion": {}
+    "test.deletion": {"enabled": false}
--- a/tests/test_legacy.py
+++ /dev/null
@@
-def test_legacy() -> None:
-    assert run() == 1
```

Incorrect diff — the selected test disappears without such an exception, so the
deletion is reported:

```diff
--- a/tests/test_contract.py
+++ /dev/null
@@
-def test_contract() -> None:
-    assert run() == 1
```

**Options**: `enabled` (boolean, default `true`); `paths` (tuple of strings,
default the same 20 test-path globs as `test.integrity`); `snapshot_paths` (tuple
of strings, default `("*.snap", "__snapshots__/*", "*/__snapshots__/*")`);
`removed_assertions` (integer, at least 0, default `3`). Paths use the shared
case-sensitive `fnmatchcase` dialect: `*` spans `/`, and `**` or brace alternation
have no special meaning. An empty tuple selects no files, and the assertion
threshold uses strict `>` semantics, so a fall equal to the limit passes.

Correct — permit four removed assertions and restrict snapshots to one directory:

```json
{
  "checkers": {
    "test.deletion": {
      "removed_assertions": 4,
      "snapshot_paths": ["tests/snapshots/*"]
    }
  }
}
```

Incorrect — `globs` is not an option and unknown options are rejected:

```json
{
  "checkers": {
    "test.deletion": {"globs": ["tests/*"]}
  }
}
```

The gate proves only that baseline test evidence disappeared or that a snapshot
path changed. It does not prove that a replacement test covers the same behavior,
and it cannot distinguish a deliberate obsolete-test deletion from an accidental
one; review remains necessary.
