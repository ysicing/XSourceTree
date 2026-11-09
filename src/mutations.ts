export async function settleMutation(
  operation: () => Promise<void>,
  refresh: () => Promise<void>,
): Promise<{ operationError: unknown; refreshError: unknown }> {
  let operationError: unknown = null;
  let refreshError: unknown = null;
  try {
    await operation();
  } catch (error) {
    operationError = error;
  }
  try {
    await refresh();
  } catch (error) {
    refreshError = error;
  }
  return { operationError, refreshError };
}
