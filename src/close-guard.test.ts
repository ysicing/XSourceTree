import { expect, test } from "bun:test";
import { createCloseGuard } from "./close-guard";

test("an active Git operation blocks restart without clearing its busy state", () => {
  let busy = true;
  const guard = createCloseGuard({
    busy: () => busy,
    hasDrafts: () => false,
    freeze: (frozen) => { busy = frozen; },
  });
  expect(guard.prepare()).toBe("busy");
  guard.cancel();
  expect(busy).toBe(true);
  busy = false;
  expect(guard.prepare()).toBe("ready");
});

test("draft confirmation freezes writes and cancellation restores editing", () => {
  let frozen = false;
  let hasDrafts = true;
  const guard = createCloseGuard({
    busy: () => frozen,
    hasDrafts: () => hasDrafts,
    freeze: (value) => { frozen = value; },
  });
  expect(guard.prepare()).toBe("drafts");
  expect(frozen).toBe(true);
  expect(guard.prepare()).toBe("drafts");
  guard.cancel();
  expect(frozen).toBe(false);
  hasDrafts = false;
  expect(guard.prepare()).toBe("ready");
  expect(frozen).toBe(true);
});
