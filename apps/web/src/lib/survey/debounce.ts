export type DebouncedFunction<T extends (...args: never[]) => void> = ((...args: Parameters<T>) => void) & {
  cancel: () => void;
  /** Runs the pending call now, if there is one, instead of when the delay ends. */
  flush: () => void;
};

export const debounce = <T extends (...args: never[]) => void>(
  callback: T,
  delay: number
): DebouncedFunction<T> => {
  let timeoutId: ReturnType<typeof setTimeout> | undefined;
  let pendingArgs: Parameters<T> | undefined;

  const run = () => {
    const args = pendingArgs;
    timeoutId = undefined;
    pendingArgs = undefined;
    if (args) callback(...args);
  };

  const debounced = ((...args: Parameters<T>) => {
    if (timeoutId !== undefined) {
      clearTimeout(timeoutId);
    }

    pendingArgs = args;
    timeoutId = setTimeout(run, delay);
  }) as DebouncedFunction<T>;

  debounced.cancel = () => {
    if (timeoutId !== undefined) {
      clearTimeout(timeoutId);
      timeoutId = undefined;
    }
    pendingArgs = undefined;
  };

  debounced.flush = () => {
    if (timeoutId === undefined) return;
    clearTimeout(timeoutId);
    run();
  };

  return debounced;
};
