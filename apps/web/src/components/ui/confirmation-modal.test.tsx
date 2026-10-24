import { cleanup, fireEvent, render } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { ConfirmationModal } from "./confirmation-modal";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods; these stand in for what the Dialog reads of them.
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(cleanup);

const renderModal = (props: { closeOnOutsideClick?: boolean; buttonLoading?: boolean }) => {
  const setOpen = vi.fn();
  const onCancel = vi.fn();
  const { container } = render(
    <I18nextProvider i18n={i18n}>
      <ConfirmationModal
        title="Discard changes?"
        open
        setOpen={setOpen}
        onConfirm={() => {}}
        body="body"
        buttonText="Save"
        onCancel={onCancel}
        {...props}
      />
    </I18nextProvider>
  );
  const dialog = container.querySelector("dialog");
  if (!dialog) throw new Error("no dialog rendered");
  // What the browser fires at a modal dialog when Escape is pressed.
  const pressEscape = () => fireEvent(dialog, new Event("cancel", { cancelable: true }));
  return { setOpen, onCancel, pressEscape };
};

test.each([
  [true, false],
  [false, false],
  [true, true],
  [false, true],
])(
  "Escape closes the confirmation (closeOnOutsideClick=%s, buttonLoading=%s), like its Cancel button",
  (closeOnOutsideClick, buttonLoading) => {
    const { setOpen, pressEscape } = renderModal({ closeOnOutsideClick, buttonLoading });
    pressEscape();
    expect(setOpen).toHaveBeenCalledWith(false);
  }
);

test("Escape does not run onCancel, which may discard the caller's work", () => {
  const { onCancel, pressEscape } = renderModal({ closeOnOutsideClick: false });
  pressEscape();
  expect(onCancel).not.toHaveBeenCalled();
});
