import { expect, test } from "bun:test";
import { sideBySideRows } from "./diff-layout";
import { parseDiff } from "./presentation";

test("side by side aligns replacement runs and preserves source change IDs", () => {
  const rows = sideBySideRows(parseDiff("@@ -1,4 +1,3 @@\n keep\n-old one\n-old two\n-old three\n+new one\n+new two\n"));
  expect(rows[0]?.kind).toBe("shared");
  const pairs = rows.filter((row) => row.kind === "pair");
  expect(pairs.map((row) => [row.before?.row.before ?? null, row.after?.row.after ?? null])).toEqual([
    [1, 1], [2, 2], [3, 3], [4, null],
  ]);
  expect(pairs.map((row) => [row.before?.row.changeID ?? null, row.after?.row.changeID ?? null])).toEqual([
    [null, null], [0, 3], [1, 4], [2, null],
  ]);
});

test("side by side keeps absent sides empty for pure additions and deletions", () => {
  const rows = sideBySideRows(parseDiff("@@ -0,0 +1,2 @@\n+one\n+two\n@@ -8,2 +9,0 @@\n-three\n-four\n"));
  const pairs = rows.filter((row) => row.kind === "pair");
  expect(pairs.map((row) => [row.before?.row.content ?? null, row.after?.row.content ?? null])).toEqual([
    [null, "+one"], [null, "+two"], ["-three", null], ["-four", null],
  ]);
});

test("no newline notes stay on their side without breaking replacement alignment", () => {
  const source = parseDiff("@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n");
  const original = JSON.stringify(source);
  const rows = sideBySideRows(source);
  expect(rows).toHaveLength(2);
  const pair = rows[1]!;
  expect(pair.kind).toBe("pair");
  if (pair.kind === "pair") {
    expect(pair.before?.note).toBe("\\ No newline at end of file");
    expect(pair.after?.note).toBe("\\ No newline at end of file");
    expect(pair.before?.row.changeID).toBe(0);
    expect(pair.after?.row.changeID).toBe(1);
  }
  expect(JSON.stringify(source)).toBe(original);
});

test("file and hunk metadata prevent changes in different blocks from pairing", () => {
  const rows = sideBySideRows(parseDiff("diff --git a/a b/a\n@@ -1 +0,0 @@\n-a\ndiff --git a/b b/b\n@@ -0,0 +1 @@\n+b\n"));
  expect(rows.filter((row) => row.kind === "shared")).toHaveLength(4);
  const pairs = rows.filter((row) => row.kind === "pair");
  expect(pairs[0]?.after).toBeNull();
  expect(pairs[1]?.before).toBeNull();
  expect(sideBySideRows([])).toEqual([]);
});
