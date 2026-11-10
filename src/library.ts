/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

type Repository = { name: string; path: string; branch: string; groupID: string };
type Group = { id: string; name: string };

export function filterRepositories<Entry extends Repository>(entries: Entry[], groups: Group[], groupID: string, query: string): Entry[] {
  const text = query.trim().toLocaleLowerCase();
  const names = new Map(groups.map((group) => [group.id, group.name]));
  return entries.filter((entry) => {
    if (groupID !== "ALL" && entry.groupID !== groupID) return false;
    return !text || [entry.name, entry.path, entry.branch, names.get(entry.groupID) ?? ""]
      .some((value) => value.toLocaleLowerCase().includes(text));
  }).sort((first, second) => first.name.localeCompare(second.name) || first.path.localeCompare(second.path));
}
