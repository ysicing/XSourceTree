import { expect, test } from "bun:test";
import { checkedPaths, visibleFiles, reconcileChecked } from "./files";

const files = [
  { path: "z/a.ts", index: "M", working: "M", conflict: false },
  { path: "a/z.ts", index: "?", working: "?", conflict: false },
  { path: "conflict.ts", index: "U", working: "U", conflict: true },
];

test("filtered batch actions exclude hidden and conflicting files", () => {
  const visible = visibleFiles(files, "a/", "path");
  expect(checkedPaths(visible, new Set(["z/a.ts", "a/z.ts", "conflict.ts"]))).toEqual(["a/z.ts"]);
  expect(checkedPaths(files, new Set(files.map((file) => file.path)))).toEqual(["z/a.ts", "a/z.ts"]);
});

test("refresh drops files no longer in the relevant staging group", () => {
  expect([...reconcileChecked(new Set(["gone", "z/a.ts", "conflict.ts"]), files)]).toEqual(["z/a.ts"]);
  expect(visibleFiles(files, ".TS", "name").map((file) => file.path)).toEqual(["z/a.ts", "conflict.ts", "a/z.ts"]);
});
