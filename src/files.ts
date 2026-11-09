type FileEntry = { path: string; conflict: boolean };
export type FileSort = "path" | "name";

export function visibleFiles<T extends FileEntry>(files: T[], query: string, sort: FileSort): T[] {
  const key = (file: T) => sort === "name" ? file.path.split("/").pop()! : file.path;
  return files.filter((file) => file.path.toLocaleLowerCase().includes(query.toLocaleLowerCase()))
    .sort((first, second) => key(first).localeCompare(key(second)) || first.path.localeCompare(second.path));
}

export function checkedPaths(files: FileEntry[], checked: Set<string>): string[] {
  return files.filter((file) => !file.conflict && checked.has(file.path)).map((file) => file.path);
}

export function reconcileChecked(checked: Set<string>, files: FileEntry[]): Set<string> {
  return new Set(checkedPaths(files, checked));
}
