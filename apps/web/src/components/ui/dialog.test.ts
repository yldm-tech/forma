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

const handlers = (disableCloseOnOutsideClick = false, allowEscapeClose?: boolean) => {
  const changes: boolean[] = [];
  return {
    changes,
    ...dialogEventHandlers({
      open: true,
      onOpenChange: (o) => changes.push(o),
      disableCloseOnOutsideClick,
      allowEscapeClose,
    }),
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

test.each([
  // [disableCloseOnOutsideClick, allowEscapeClose, Escape closes]
  [false, false, true],
  [false, true, true],
  [true, false, false],
  [true, true, true],
])(
  "Escape with disableCloseOnOutsideClick=%s and allowEscapeClose=%s closes the dialog: %s",
  (disableCloseOnOutsideClick, allowEscapeClose, closes) => {
    const h = handlers(disableCloseOnOutsideClick, allowEscapeClose);
    const cancel = event(outer);
    h.onCancel(cancel);
    expect(h.changes).toEqual(closes ? [false] : []);
    // The native close is always cancelled; the dialog closes through onOpenChange, so React state stays the source of truth.
    expect(cancel.prevented).toBe(true);
  }
);

test("allowEscapeClose still ignores a nested dialog's Escape", () => {
  const h = handlers(true, true);
  const cancel = event(inner);
  h.onCancel(cancel);
  expect(h.changes).toEqual([]);
  expect(cancel.prevented).toBe(false);
});
