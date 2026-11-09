import { expect, test } from "bun:test";
import { escapeHTML, fileGroups, graphRows, parseDiff } from "./presentation";

test("unified diff tracks old and new line numbers independently", () => {
  const rows = parseDiff("--- a/file\n+++ b/file\n@@ -3,2 +3,2 @@\n same\n-old\n+new\n\\ No newline at end of file\n");
  expect(rows[3]).toEqual({ kind: "context", content: " same", before: 3, after: 3 });
  expect(rows[4]).toEqual({ kind: "removed", content: "-old", before: 4, after: null });
  expect(rows[5]).toEqual({ kind: "added", content: "+new", before: null, after: 4 });
  expect(rows[6]?.before).toBeNull();
});

test("repository content is escaped before entering HTML", () => {
  expect(escapeHTML('<img src=x onerror="alert(1)"> & \'')).toBe("&lt;img src=x onerror=&quot;alert(1)&quot;&gt; &amp; &#39;");
});

test("partially staged files appear in both groups and conflicts remain unstaged", () => {
  const files = [
    { path: "both", index: "M", working: "M", conflict: false },
    { path: "new", index: "?", working: "?", conflict: false },
    { path: "conflict", index: "U", working: "U", conflict: true },
  ];
  const groups = fileGroups(files);
  expect(groups.staged.map((file) => file.path)).toEqual(["both"]);
  expect(groups.working.map((file) => file.path)).toEqual(["both", "new", "conflict"]);
});

test("commit graph connects both merge parents and converges shared ancestry", () => {
  const rows = graphRows([
    { hash: "merge", parents: ["main", "feature"] },
    { hash: "feature", parents: ["main"] },
    { hash: "main", parents: [] },
  ]);
  expect(rows[0]?.edges).toEqual([{ from: 0, to: 0 }, { from: 0, to: 1 }]);
  expect(rows[1]?.column).toBe(1);
  expect(rows[1]?.edges).toEqual([{ from: 0, to: 0 }, { from: 1, to: 0 }]);
  expect(rows[2]?.column).toBe(0);
  expect(rows[2]?.edges).toEqual([]);
});
