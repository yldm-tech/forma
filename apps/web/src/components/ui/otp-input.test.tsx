import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { OTPInput } from "./otp-input";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const renderOtp = (autoFocus?: boolean) =>
  render(
    <I18nextProvider i18n={i18n}>
      {/* oxlint-disable-next-line jsx-a11y/no-autofocus -- the prop under test */}
      <OTPInput value="" valueLength={6} onChange={() => {}} autoFocus={autoFocus} />
    </I18nextProvider>
  );

const box = (number: number) =>
  screen.getByLabelText(i18n.t("common.digit_number_of_total", { number, total: 6 }));

test("autoFocus puts the focus in the first box, and marks it for a Dialog that opens around it", () => {
  renderOtp(true);
  expect(document.activeElement).toBe(box(1));
  expect(box(1).hasAttribute("data-autofocus")).toBe(true);
  expect(box(2).hasAttribute("data-autofocus")).toBe(false);
});

test("without autoFocus the focus stays where it was", () => {
  renderOtp();
  expect(document.activeElement).toBe(document.body);
  expect(box(1).hasAttribute("data-autofocus")).toBe(false);
});
