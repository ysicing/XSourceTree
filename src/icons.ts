const paths: Record<string, string> = {
  branch: '<path d="M6 3v12a4 4 0 0 0 4 4h2"/><circle cx="6" cy="3" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="6" r="2"/><path d="M18 8a8 8 0 0 1-8 8H6"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10H3z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  minus: '<path d="M5 12h14"/>',
  refresh: '<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1"/>',
  down: '<path d="M12 3v12m-5-5 5 5 5-5M5 17v4h14v-4"/>',
  up: '<path d="M12 15V3m-5 5 5-5 5 5M5 17v4h14v-4"/>',
  cloud: '<path d="M7 18a5 5 0 1 1 0-10 6 6 0 0 1 11 2 4 4 0 0 1 0 8M12 12v9m-3-3 3 3 3-3"/>',
  history: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  changes: '<path d="M8 3H5v18h14V7l-4-4H8zM14 3v5h5M8 13h8M8 17h5"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  chevron: '<path d="m9 5 7 7-7 7"/>',
  search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 5 5"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  commit: '<circle cx="12" cy="12" r="4"/><path d="M2 12h6m8 0h6"/>',
  file: '<path d="M6 3h8l4 4v14H6zM14 3v5h4"/>',
  warning: '<path d="m12 3 10 18H2zM12 9v5m0 3v1"/>',
  stash: '<path d="M3 4h18v5H3zM5 9v11h14V9M9 13h6"/>',
  merge: '<circle cx="6" cy="4" r="2"/><circle cx="6" cy="20" r="2"/><circle cx="18" cy="4" r="2"/><path d="M6 6v12M18 6v3c0 5-12 3-12 8"/>',
  more: '<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
};

export function icon(name: string, size = 18): string {
  return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] ?? paths.file}</svg>`;
}
