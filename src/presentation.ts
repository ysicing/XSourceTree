/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

export function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[character]!);
}

export type DiffRow = {
  kind: "meta" | "hunk" | "context" | "added" | "removed";
  content: string;
  before: number | null;
  after: number | null;
  changeID?: number;
};

export function parseDiff(text: string): DiffRow[] {
  let before = 0;
  let after = 0;
  let inHunk = false;
  let changeID = 0;
  return text.replace(/\n$/, "").split("\n").map((content): DiffRow => {
    const header = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(content);
    if (header) {
      before = Number(header[1]);
      after = Number(header[2]);
      inHunk = true;
      return { kind: "hunk", content, before: null, after: null };
    }
    if (inHunk && content.startsWith("+")) {
      return { kind: "added", content, before: null, after: after++, changeID: changeID++ };
    }
    if (inHunk && content.startsWith("-")) {
      return { kind: "removed", content, before: before++, after: null, changeID: changeID++ };
    }
    if (inHunk && content.startsWith(" ")) {
      return { kind: "context", content, before: before++, after: after++ };
    }
    if (!content.startsWith("\\")) inHunk = false;
    return { kind: "meta", content, before: null, after: null };
  });
}

type FileState = { path: string; index: string; working: string; conflict: boolean };

export function fileGroups<T extends FileState>(files: T[]): { staged: T[]; working: T[] } {
  return {
    staged: files.filter((file) => !file.conflict && file.index !== " " && file.index !== "?"),
    working: files.filter((file) => file.conflict || file.working !== " "),
  };
}

export type GraphRow = {
  column: number;
  incoming: number[];
  edges: { from: number; to: number }[];
  columns: number;
};

export function graphRows(commits: { hash: string; parents: string[] }[]): GraphRow[] {
  let lanes: string[] = [];
  return commits.map((commit) => {
    if (!lanes.includes(commit.hash)) lanes.push(commit.hash);
    const before = [...lanes];
    const column = before.indexOf(commit.hash);
    const after = before.filter((hash) => hash !== commit.hash);
    for (const [index, parent] of commit.parents.entries()) {
      if (!after.includes(parent)) {
        if (index === 0) after.splice(Math.min(column, after.length), 0, parent);
        else after.push(parent);
      }
    }
    const edges = before.flatMap((hash, from) => {
      if (hash === commit.hash) {
        return commit.parents.map((parent) => ({ from, to: after.indexOf(parent) }));
      }
      return [{ from, to: after.indexOf(hash) }];
    });
    lanes = after;
    return {
      column, incoming: before.map((_, index) => index), edges,
      columns: Math.max(before.length, after.length),
    };
  });
}
