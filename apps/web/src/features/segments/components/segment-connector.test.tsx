import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { SegmentConnector } from "./segment-connector";

afterEach(cleanup);

const renderConnector = (i18n: I18n, connector: "and" | "or" | null, onToggle = vi.fn()) => {
  render(
    <I18nextProvider i18n={i18n}>
      <SegmentConnector connector={connector} onToggle={onToggle} />
    </I18nextProvider>
  );
  return onToggle;
};

test("the first row's Where is plain text, not a button", async () => {
  const i18n = await initI18n("en-US");
  renderConnector(i18n, null);
  expect(screen.getByText(i18n.t("workspace.segments.where"))).toBeTruthy();
  expect(screen.queryByRole("button")).toBeNull();
});

test("the connector is translated, says what a click does, and switches to the other", async () => {
  const i18n = await initI18n("ja-JP");
  const onToggle = renderConnector(i18n, "or");
  const button = screen.getByRole("button", {
    name: i18n.t("workspace.segments.toggle_connector", {
      current: i18n.t("workspace.segments.connector_or"),
      next: i18n.t("workspace.segments.connector_and"),
    }),
  });
  expect(button.textContent).toBe("または");
  fireEvent.click(button);
  expect(onToggle).toHaveBeenCalledWith("and");
});
