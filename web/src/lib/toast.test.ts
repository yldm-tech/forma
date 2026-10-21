import { expect, test } from "vite-plus/test";
import { createToastStore } from "./toast";

const manualTimers = () => {
  const pending: { fn: () => void; ms: number }[] = [];
  return {
    schedule: (fn: () => void, ms: number) => pending.push({ fn, ms }),
    run: () => pending.splice(0).forEach(({ fn }) => fn()),
    pending,
  };
};

test("a toast expires after its kind's duration; a loading toast stays", () => {
  const timers = manualTimers();
  const store = createToastStore(timers.schedule);
  store.success("saved");
  store.loading("working");
  expect(store.getSnapshot().map((t) => t.message)).toEqual(["saved", "working"]);
  expect(timers.pending.map((p) => p.ms)).toEqual([4000]);
  timers.run();
  expect(store.getSnapshot().map((t) => t.message)).toEqual(["working"]);
});

test("an id replaces the toast in place, and the earlier timer does not remove the replacement", () => {
  const timers = manualTimers();
  const store = createToastStore(timers.schedule);
  const id = store.loading("inviting");
  store.success("invited", { id });
  expect(store.getSnapshot()).toEqual([{ id, kind: "success", message: "invited" }]);
  store.error("failed", { id });
  timers.pending.shift()?.fn();
  expect(store.getSnapshot()).toHaveLength(1);
  timers.run();
  expect(store.getSnapshot()).toHaveLength(0);
});

test("subscribers hear every change", () => {
  const store = createToastStore(() => undefined);
  let calls = 0;
  const unsubscribe = store.subscribe(() => calls++);
  const id = store.error("boom");
  store.dismiss(id);
  store.dismiss(id);
  unsubscribe();
  store.success("after");
  expect(calls).toBe(2);
});
