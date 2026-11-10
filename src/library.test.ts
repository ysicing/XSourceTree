/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import { expect, test } from "bun:test";
import { filterRepositories } from "./library";

const entries = [
  { name: "XSourceTree", path: "/Work/tools/XSourceTree", branch: "main", groupID: "tools", changes: 2 },
  { name: "NextStats", path: "/Work/apps/NextStats", branch: "feature/calendar", groupID: "apps", changes: 0 },
  { name: "<img>", path: "/Other/My Project", branch: "", groupID: "", changes: 0 },
];

test("repository search matches names, paths, branches and group names without case sensitivity", () => {
  const groups = [{ id: "tools", name: "开发工具" }, { id: "apps", name: "应用" }];
  expect(filterRepositories(entries, groups, "ALL", "xsourcetree").map((entry) => entry.name)).toEqual(["XSourceTree"]);
  expect(filterRepositories(entries, groups, "ALL", " calendar ").map((entry) => entry.name)).toEqual(["NextStats"]);
  expect(filterRepositories(entries, groups, "ALL", "my project").map((entry) => entry.name)).toEqual(["<img>"]);
  expect(filterRepositories(entries, groups, "ALL", "开发工具").map((entry) => entry.name)).toEqual(["XSourceTree"]);
});

test("group filter and repository search compose without modifying the collection", () => {
  expect(filterRepositories(entries, [], "apps", "tools")).toEqual([]);
  expect(filterRepositories(entries, [], "", "").map((entry) => entry.name)).toEqual(["<img>"]);
  expect(filterRepositories(entries, [], "ALL", "").length).toBe(3);
  expect(entries[0]?.name).toBe("XSourceTree");
});
