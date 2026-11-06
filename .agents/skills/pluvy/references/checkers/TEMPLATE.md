# Checker page template

This file is the template for a new checker page in this directory. `TEMPLATE.md`
is the only non-checker file under `checkers/`: every other page is named after a
checker ID and linked from the checker index. Copy it to
`checkers/<checker-id>.md`, fill in the placeholders, and then work through the
checklist at the end of this file.

## Format rules

Every checker page uses the same language format. The rules below are the
contract; a page that breaks one of them is a documentation defect.

1. The **Languages** line carries the scope in one sentence only. It may end with
   one short guiding phrase that announces the difference table, and it never
   states behavioral facts.
2. Per-language differences are graded by length. A difference of three or more
   sentences, or one that needs a code example, becomes a `#### <Language>`
   section. A shorter difference preferably becomes one row of a
   `| Language | <column> |` table; existing pages keep their frozen forms.
   Count sentences by their closing period: a language name prefix does not
   count as a sentence, and semicolons do not split a sentence.
3. A language difference never appears as a bullet list — neither as standalone
   bullets nor inside a table cell or a `####` section.
4. The same fact appears in exactly one carrier per page: a table row, a `####`
   section, or common prose, never two of them.
5. Common rules — behavior that is language-independent, or every behavioral
   detail of a single-language page — is ordinary prose outside the language
   block, that is, outside the **Languages** line, the difference table and the
   `####` sections.
6. A page contains a `## Configuration` heading if and only if it contains a
   `####` language section. `## Configuration` sits immediately before the
   **Options** line and introduces the configuration content that runs from
   **Options** to the end of the file. Pages without a `####` section have no
   `## Configuration` heading.

## Skeleton

Copy the block below and replace every `<...>` placeholder. The inner fences are
shorter than the outer one so the skeleton can be copied verbatim.

````markdown
# `<checker-id>`

[Back to the checker index](../checkers.md)

Reports <one sentence stating what the checker reports>.

**Severity**: <error or warning>.

**Languages**: <one scope sentence naming the analyzers or languages that register the checker>.<Optional guiding phrase that announces the table below:>

| Language | <Difference column named after the content> |
| --- | --- |
| <Language> | <one or two sentences unique to this language> |

<Common rules that hold for every language, as ordinary prose outside the
language block.>

#### <Language>

<Three or more sentences, or a code example, that only apply to this language.
Delete this section and its `## Configuration` heading when no language needs
one.>

## Configuration

**Options**: <each option with its type and default value, or "None beyond
`enabled` (boolean, default `true`).">.

Correct:

```json
{
  "checkers": {
    "<checker-id>": {}
  }
}
```

Incorrect — <the reason this configuration is rejected>:

```json
{
  "checkers": {
    "<checker-id>": {"<unknown-option>": true}
  }
}
```

<What the checker does not prove and where review remains necessary, as the
closing prose of the page.>
````

The skeleton is correct as copied: the title keeps the checker ID in backticks
and the back link points at `../checkers.md`, so a copied page already resolves
its links and satisfies the contract that each page names its own checker.

## Checklist for adding a checker

1. Copy the skeleton to `checkers/<checker-id>.md` and fill it in. Keep the
   checker ID in backticks in the page title: the guidance contract test
   `test_checker_document_names_its_own_checker` asserts that backticked ID on
   every listed page.
2. Add a row to the `## Checker index` table in `checkers.md`, and update the
   `## Coverage` section of the same file with the analyzers that register the
   new diagnostic.
3. Update `.agents/skills/pluvy/SKILL.md` in the same batch: the checker
   table, the logical-checker and diagnostic-ID count sentence, and the
   base-set diagnostic-ID list.
4. Update the explicit lists in `tests/test_guidance_contract.py` — `CHECKER_IDS`
   and `DIAGNOSTIC_IDS`; the per-page link arguments are derived from the ID set.
   Missing this step fails no test: it only silently drops the contract
   protection for the new page.
5. Keep the Correct example consistent with the policy schema in
   `src/pluvy/policy/` and with `docs/checker-configuration.md`; the index,
   the checker pages and `docs/checker-configuration.md` change in the same batch.
6. Write the checker ID in backticks wherever it appears as a name, including the
   index table.

Adding the checker itself is a `src/` and `tests/` change, which is outside the
scope of this documentation set and needs its own task.
