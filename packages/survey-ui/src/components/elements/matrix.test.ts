/**
 * @vitest-environment jsdom
 */
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { Matrix, type MatrixOption, type MatrixProps } from "./matrix";

const rows: MatrixOption[] = [
  { id: "row-0", label: "Speed" },
  { id: "row-1", label: "Support" },
];

const columns: MatrixOption[] = [
  { id: "col-0", label: "Unhappy" },
  { id: "col-1", label: "Neutral" },
  { id: "col-2", label: "Satisfied" },
  { id: "col-3", label: "Delighted" },
];

// jsdom does no layout, so the widths the layout hook reads are set by hand: the card's width and the width the table needs. The measured element is the matrix container, the only element that reads them.
let size = { clientWidth: 0, scrollWidth: 0 };
let resizeCallbacks: (() => void)[] = [];

class FakeResizeObserver {
  private readonly callback: () => void;
  constructor(callback: () => void) {
    this.callback = callback;
  }
  observe(): void {
    resizeCallbacks.push(this.callback);
  }
  disconnect(): void {
    resizeCallbacks = resizeCallbacks.filter((callback) => callback !== this.callback);
  }
}

const resizeCard = (clientWidth: number, tableWidth: number): void => {
  size = { clientWidth, scrollWidth: Math.max(clientWidth, tableWidth) };
  act(() => {
    for (const callback of [...resizeCallbacks]) callback();
  });
};

beforeEach(() => {
  size = { clientWidth: 0, scrollWidth: 0 };
  resizeCallbacks = [];
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => size.clientWidth);
  vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockImplementation(() => size.scrollWidth);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const renderMatrix = (props: Partial<MatrixProps> = {}) => {
  const onChange = vi.fn();
  const view = render(
    React.createElement(Matrix, {
      elementId: "m",
      inputId: "m",
      headline: "How satisfied are you?",
      rows,
      columns,
      onChange,
      ...props,
    })
  );
  return { ...view, onChange };
};

const layoutOf = (container: HTMLElement): string | null =>
  container.querySelector("[data-matrix-layout]")?.getAttribute("data-matrix-layout") ?? null;

const radioNamesAndValues = (container: HTMLElement): string[] =>
  Array.from(container.querySelectorAll<HTMLInputElement>('input[type="radio"]')).map(
    (input) => `${input.id}|${input.name}|${input.value}`
  );

describe("Matrix layout", () => {
  test("renders a table when the columns fit the card", () => {
    size = { clientWidth: 600, scrollWidth: 600 };
    const { container } = renderMatrix();

    expect(layoutOf(container)).toBe("table");
    expect(container.querySelector("table")).not.toBeNull();
    expect(screen.getByRole("radio", { name: "Speed Delighted" })).toBeTruthy();
  });

  // At 390px the last column of a four-column satisfaction matrix sat behind a horizontal scroll, so phone respondents could answer every row without seeing it.
  test("stacks the rows when the table is wider than the card, keeping every radio's id, name and value", () => {
    size = { clientWidth: 600, scrollWidth: 600 };
    const { container: wide } = renderMatrix();
    const tableRadios = radioNamesAndValues(wide);
    cleanup();

    size = { clientWidth: 313, scrollWidth: 378 };
    const { container } = renderMatrix();

    expect(layoutOf(container)).toBe("stacked");
    expect(container.querySelector("table")).toBeNull();
    expect(radioNamesAndValues(container)).toEqual(tableRadios);

    const speed = screen.getByRole("group", { name: "Speed" });
    expect(within(speed).getAllByRole("radio")).toHaveLength(columns.length);
    expect(within(speed).getByRole("radio", { name: "Delighted" })).toBeTruthy();
    // A stacked option rings its whole row like a single-select option; the dot does not ring as well.
    expect(container.querySelector("[data-fb-focus-ring]")).toBeNull();
  });

  test("a stacked option selects its column for its row", () => {
    size = { clientWidth: 313, scrollWidth: 378 };
    const { onChange } = renderMatrix({ value: { "row-0": "col-0" } });

    fireEvent.click(within(screen.getByRole("group", { name: "Support" })).getByText("Delighted"));

    expect(onChange).toHaveBeenCalledWith({ "row-0": "col-0", "row-1": "col-3" });
  });

  test("goes back to the table once the card is as wide as the table needs", () => {
    size = { clientWidth: 313, scrollWidth: 378 };
    const { container } = renderMatrix();
    expect(layoutOf(container)).toBe("stacked");

    // Narrower than the table: a scrollbar appearing or a small rotation must not flip it back.
    resizeCard(370, 0);
    expect(layoutOf(container)).toBe("stacked");

    resizeCard(380, 378);
    expect(layoutOf(container)).toBe("table");
  });

  // Switching layouts replaces every radio, so without this a rotation or a window resize dropped keyboard focus to the page and the next Tab started over at the first row.
  test("keeps focus on the same radio when the layout switches", () => {
    size = { clientWidth: 600, scrollWidth: 600 };
    const { container } = renderMatrix();
    const radio = screen.getByRole("radio", { name: "Support Satisfied" });
    const radioId = radio.id;
    radio.focus();

    resizeCard(313, 378);
    expect(layoutOf(container)).toBe("stacked");
    expect(document.activeElement?.id).toBe(radioId);
    expect(document.activeElement).not.toBe(radio);

    resizeCard(400, 378);
    expect(layoutOf(container)).toBe("table");
    expect(document.activeElement?.id).toBe(radioId);
  });

  test("leaves focus alone when it is outside the matrix as the layout switches", () => {
    size = { clientWidth: 600, scrollWidth: 600 };
    const { container } = renderMatrix();
    const outside = document.createElement("button");
    document.body.append(outside);
    outside.focus();

    resizeCard(313, 378);
    expect(layoutOf(container)).toBe("stacked");
    expect(document.activeElement).toBe(outside);
    outside.remove();
  });

  test("measures the table again when the labels change", () => {
    size = { clientWidth: 313, scrollWidth: 378 };
    const { container, rerender } = renderMatrix();
    expect(layoutOf(container)).toBe("stacked");

    // Shorter labels (another language) fit the same card.
    size = { clientWidth: 313, scrollWidth: 313 };
    rerender(
      React.createElement(Matrix, {
        elementId: "m",
        inputId: "m",
        headline: "How satisfied are you?",
        rows,
        columns: columns.map((column) => ({ ...column, label: column.label.slice(0, 3) })),
        onChange: () => undefined,
      })
    );
    expect(layoutOf(container)).toBe("table");
  });

  test("each table cell's whole area is the radio's label, 44px tall but no wider than the cell was before", () => {
    size = { clientWidth: 600, scrollWidth: 600 };
    const { container } = renderMatrix();

    const radio = screen.getByRole("radio", { name: "Support Neutral" });
    const label = container.querySelector<HTMLLabelElement>(`label[for="${radio.id}"]`);
    expect(label?.parentElement?.tagName).toBe("TD");
    expect(label?.className).toContain("min-h-11");
    // The cell's minimum width sets the table's, and with it the card width below which the rows stack. It stays 32px, what the 16px dot plus p-2 gave before, so a five-point scale that fitted a phone as a table still does; a 44px floor stacked it into a list three times as tall.
    expect(label?.className).toContain("min-w-8");
    expect(label?.className).not.toContain("min-w-11");
    expect(label?.className).toContain("before:inset-0");
    expect(label?.querySelector("[data-fb-focus-ring]")).not.toBeNull();
  });
});
