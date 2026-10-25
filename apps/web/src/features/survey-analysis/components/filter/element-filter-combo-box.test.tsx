import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { ElementFilterComboBox } from "./element-filter-combo-box";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("ja-JP");
  // cmdk scrolls the selected item into view and measures its list; jsdom lays nothing out.
  Element.prototype.scrollIntoView ??= () => {};
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(cleanup);

const renderComboBox = (props: Partial<Parameters<typeof ElementFilterComboBox>[0]>) => {
  const onChangeFilterValue = vi.fn();
  const onChangeFilterComboBoxValue = vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <ElementFilterComboBox
        filterOptions={undefined}
        filterComboBoxOptions={undefined}
        filterValue={undefined}
        filterComboBoxValue={undefined}
        onChangeFilterValue={onChangeFilterValue}
        onChangeFilterComboBoxValue={onChangeFilterComboBoxValue}
        handleRemoveMultiSelect={vi.fn()}
        {...props}
      />
    </I18nextProvider>
  );
  return { onChangeFilterValue, onChangeFilterComboBoxValue };
};

// The row's operator and value are wire constants; ja-JP shows their labels and still hands the constants back.
describe("ElementFilterComboBox under ja-JP", () => {
  test("shows the selected operator and value by their labels", () => {
    renderComboBox({
      type: TSurveyElementTypeEnum.OpenText,
      filterOptions: ["is"],
      filterComboBoxOptions: ["Filled out", "Skipped"],
      filterValue: "is",
      filterComboBoxValue: "Skipped",
    });
    expect(screen.getByText(i18n.t("workspace.analysis.filter_op.is"))).toBeTruthy();
    expect(screen.getByText(i18n.t("common.skipped"))).toBeTruthy();
    expect(screen.queryByText("Skipped")).toBeNull();
  });

  test("lists the values by their labels and selects the constant", () => {
    const { onChangeFilterComboBoxValue } = renderComboBox({
      type: TSurveyElementTypeEnum.OpenText,
      filterOptions: ["is"],
      filterComboBoxOptions: ["Filled out", "Skipped"],
      filterValue: "is",
    });
    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.select") }));
    fireEvent.click(screen.getByText(i18n.t("workspace.analysis.filter_op.filled_out")));
    expect(onChangeFilterComboBoxValue).toHaveBeenCalledWith("Filled out");
  });

  test("a score operator is shown translated in the trigger", () => {
    renderComboBox({
      type: TSurveyElementTypeEnum.Rating,
      filterOptions: ["Is equal to", "Is less than", "Is more than", "Submitted", "Skipped"],
      filterComboBoxOptions: ["1", "2", "3"],
      filterValue: "Is more than",
    });
    expect(screen.getByText(i18n.t("workspace.analysis.filter_op.is_more_than"))).toBeTruthy();
    expect(screen.queryByText("Is more than")).toBeNull();
  });

  test("Submitted and Skipped leave a CES value box disabled", () => {
    renderComboBox({
      type: TSurveyElementTypeEnum.CES,
      filterOptions: ["Is equal to", "Submitted", "Skipped"],
      filterComboBoxOptions: ["1", "2", "3", "4", "5"],
      filterValue: "Submitted",
    });
    expect(
      (screen.getByRole("button", { name: i18n.t("common.select") }) as HTMLButtonElement).disabled
    ).toBe(true);
  });
});
