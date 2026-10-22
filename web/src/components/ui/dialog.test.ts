import { expect, test } from "vite-plus/test";
import { dialogEventHandlers } from "./dialog";

const outer = { id: "outer" } as unknown as EventTarget;
const inner = { id: "inner" } as unknown as EventTarget;

const event = (target: EventTarget) => {
  const e = {
    target,
    currentTarget: outer,
    prevented: false,
    preventDefault: () => void (e.prevented = true),
  };
  return e;
};

const handlers = (disableCloseOnOutsideClick = false) => {
  const changes: boolean[] = [];
  return {
    changes,
    ...dialogEventHandlers({ open: true, onOpenChange: (o) => changes.push(o), disableCloseOnOutsideClick }),
  };
};

test("a nested dialog's close and Escape leave the outer dialog open", () => {
  const h = handlers();
  const cancel = event(inner);
  h.onCancel(cancel);
  h.onClose(event(inner));
  expect(h.changes).toEqual([]);
  // The nested dialog's own Escape is not cancelled by the outer one.
  expect(cancel.prevented).toBe(false);
});

test("the dialog's own close and Escape close it", () => {
  const h = handlers();
  const cancel = event(outer);
  h.onCancel(cancel);
  h.onClose(event(outer));
  expect(h.changes).toEqual([false, false]);
  expect(cancel.prevented).toBe(true);
});

test("Escape is ignored when the dialog holds unsaved input", () => {
  const h = handlers(true);
  const cancel = event(outer);
  h.onCancel(cancel);
  expect(h.changes).toEqual([]);
  expect(cancel.prevented).toBe(true);
});
