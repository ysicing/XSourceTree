type CloseReason = "ready" | "busy" | "drafts";

export function createCloseGuard(options: {
  busy: () => boolean;
  hasDrafts: () => boolean;
  freeze: (frozen: boolean) => void;
}) {
  let prepared: CloseReason | null = null;
  return {
    prepare(): CloseReason {
      if (prepared) return prepared;
      if (options.busy()) return "busy";
      prepared = options.hasDrafts() ? "drafts" : "ready";
      options.freeze(true);
      return prepared;
    },
    cancel(): void {
      if (!prepared) return;
      prepared = null;
      options.freeze(false);
    },
  };
}
