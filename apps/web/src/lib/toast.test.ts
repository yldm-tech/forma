import { expect, test } from "vite-plus/test";
import { ACTION_TOAST_DURATION_MS, createToastStore, createToasterHosts } from "./toast";

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

test("only the newest toaster host shows the toasts, and closing it hands them back", () => {
  const hosts = createToasterHosts();
  const page = hosts.register("page");
  expect(hosts.getSnapshot()).toBe("page");
  const dialog = hosts.register("dialog");
  const nested = hosts.register("nested");
  expect(hosts.getSnapshot()).toBe("nested");
  nested();
  expect(hosts.getSnapshot()).toBe("dialog");
  dialog();
  expect(hosts.getSnapshot()).toBe("page");
  page();
  expect(hosts.getSnapshot()).toBeNull();
});

test("a toast with an action keeps it and stays longer", () => {
  const timers = manualTimers();
  const store = createToastStore(timers.schedule);
  const action = { label: "Undo", onClick: () => {} };
  store.success("archived", { action });
  expect(store.getSnapshot()[0]?.action).toBe(action);
  expect(timers.pending.map((p) => p.ms)).toEqual([ACTION_TOAST_DURATION_MS]);
});

test("a hovered or focused toast does not expire, and gets its full duration again once left", () => {
  const timers = manualTimers();
  const store = createToastStore(timers.schedule);
  const id = store.success("archived", { action: { label: "Undo", onClick: () => {} } });
  store.pause(id);
  timers.run();
  expect(store.getSnapshot()).toHaveLength(1);
  store.resume(id);
  expect(timers.pending.map((p) => p.ms)).toEqual([ACTION_TOAST_DURATION_MS]);
  timers.run();
  expect(store.getSnapshot()).toHaveLength(0);
});

test("a timer from before a pause does not end the toast after it resumes", () => {
  const timers = manualTimers();
  const store = createToastStore(timers.schedule);
  const id = store.success("saved");
  const [first] = timers.pending.splice(0);
  store.pause(id);
  store.resume(id);
  first?.fn();
  expect(store.getSnapshot()).toHaveLength(1);
  timers.run();
  expect(store.getSnapshot()).toHaveLength(0);
});

test("a toast replaced while paused waits for the resume, and resuming a toast that is not paused starts nothing", () => {
  const timers = manualTimers();
  const store = createToastStore(timers.schedule);
  const id = store.loading("copying");
  store.pause(id);
  store.success("copied", { id });
  expect(timers.pending).toHaveLength(0);
  store.resume(id);
  store.resume(id);
  expect(timers.pending.map((p) => p.ms)).toEqual([4000]);
});
