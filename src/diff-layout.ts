import type { DiffRow } from "./presentation";

export type DiffCell = { row: DiffRow; note?: string };
export type SideBySideRow =
  | { kind: "pair"; before: DiffCell | null; after: DiffCell | null }
  | { kind: "shared"; row: DiffRow };

export function sideBySideRows(rows: DiffRow[]): SideBySideRow[] {
  const result: SideBySideRow[] = [];
  let removed: DiffCell[] = [];
  let added: DiffCell[] = [];
  let previous: DiffCell[] = [];
  const flush = () => {
    for (let index = 0; index < Math.max(removed.length, added.length); index++) {
      result.push({ kind: "pair", before: removed[index] ?? null, after: added[index] ?? null });
    }
    removed = [];
    added = [];
  };
  for (const row of rows) {
    if (row.kind === "removed" || row.kind === "added") {
      const cell = { row };
      (row.kind === "removed" ? removed : added).push(cell);
      previous = [cell];
    } else if (row.kind === "meta" && row.content === "\\ No newline at end of file" && previous.length) {
      for (const cell of previous) cell.note = row.content;
    } else {
      flush();
      if (row.kind === "context") {
        const before = { row };
        const after = { row };
        result.push({ kind: "pair", before, after });
        previous = [before, after];
      } else {
        result.push({ kind: "shared", row });
        previous = [];
      }
    }
  }
  flush();
  return result;
}
