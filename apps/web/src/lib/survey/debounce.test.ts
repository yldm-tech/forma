import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { debounce } from "./debounce";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("debounce", () => {
  test("calls once with the last arguments after the delay", () => {
    const calls: string[] = [];
    const debounced = debounce((value: string) => calls.push(value), 100);

    debounced("a");
    debounced("ab");
    vi.advanceTimersByTime(99);
    expect(calls).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(calls).toEqual(["ab"]);
  });

  test("flush runs the pending call at once, and only once", () => {
    const calls: string[] = [];
    const debounced = debounce((value: string) => calls.push(value), 100);

    debounced("Reporting");
    debounced.flush();
    expect(calls).toEqual(["Reporting"]);

    vi.advanceTimersByTime(200);
    debounced.flush();
    expect(calls).toEqual(["Reporting"]);
  });

  test("flush with nothing pending does nothing", () => {
    const calls: string[] = [];
    const debounced = debounce((value: string) => calls.push(value), 100);

    debounced.flush();
    debounced("a");
    vi.advanceTimersByTime(100);
    debounced.flush();
    expect(calls).toEqual(["a"]);
  });

  test("cancel drops the pending call, so a later flush has nothing to run", () => {
    const calls: string[] = [];
    const debounced = debounce((value: string) => calls.push(value), 100);

    debounced("a");
    debounced.cancel();
    debounced.flush();
    vi.advanceTimersByTime(200);
    expect(calls).toEqual([]);
  });
});
