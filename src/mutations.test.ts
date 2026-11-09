import { expect, test } from "bun:test";
import { settleMutation } from "./mutations";

test("a failed hook still refreshes a branch that Git already switched", async () => {
  let diskBranch = "main";
  let displayedBranch = "main";
  const hookError = new Error("post-checkout failed");
  const result = await settleMutation(async () => {
    diskBranch = "feature";
    throw hookError;
  }, async () => {
    displayedBranch = diskBranch;
  });
  expect(displayedBranch).toBe("feature");
  expect(result.operationError).toBe(hookError);
  expect(result.refreshError).toBeNull();
});

test("failed reconciliation preserves both the write error and refresh error", async () => {
  const writeError = new Error("write failed");
  const refreshError = new Error("read failed");
  const result = await settleMutation(
    async () => { throw writeError; },
    async () => { throw refreshError; },
  );
  expect(result.operationError).toBe(writeError);
  expect(result.refreshError).toBe(refreshError);
});
