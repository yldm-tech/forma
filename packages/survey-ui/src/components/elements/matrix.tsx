import * as React from "react";
import { ElementError, getElementErrorAria } from "@/components/general/element-error";
import { ElementHeader } from "@/components/general/element-header";
import { Label } from "@/components/general/label";
import { cn } from "@/lib/utils";

/**
 * Option for matrix element rows and columns
 */
export interface MatrixOption {
  /** Unique identifier for the option */
  id: string;
  /** Display label for the option */
  label: string;
}

interface MatrixProps {
  /** Unique identifier for the element container */
  elementId: string;
  /** The main element or prompt text displayed as the headline */
  headline: string;
  /** Optional descriptive text displayed below the headline */
  description?: string;
  /** Unique identifier for the matrix group */
  inputId: string;
  /** Array of row options (left side) */
  rows: MatrixOption[];
  /** Array of column options (top header) */
  columns: MatrixOption[];
  /** Currently selected values: Record mapping row ID to column ID */
  value?: Record<string, string>;
  /** Callback function called when selection changes */
  onChange: (value: Record<string, string>) => void;
  /** Whether the field is required (shows asterisk indicator) */
  required?: boolean;
  /** Custom label for the required indicator */
  requiredLabel?: string;
  /** Error message to display */
  errorMessage?: string;
  /** Text direction: 'ltr' (left-to-right), 'rtl' (right-to-left), or 'auto' (auto-detect from content) */
  dir?: "ltr" | "rtl" | "auto";
  /** Whether the options are disabled */
  disabled?: boolean;
  /** Image URL to display above the headline */
  imageUrl?: string;
  /** Video URL to display above the headline */
  videoUrl?: string;
}

/**
 * Custom radio indicator driven by the sibling native input's :checked state via the Tailwind
 * `peer` utility. The native <input type="radio"> is visually hidden (sr-only) but stays in the
 * DOM as the real, focusable control; this span only paints the dot.
 */
function MatrixRadioIndicator(): React.JSX.Element {
  return (
    <span
      aria-hidden="true"
      data-fb-focus-ring
      className={cn(
        "border-control-border relative flex size-4 shrink-0 items-center justify-center rounded-full border bg-white shadow-xs transition-colors",
        "peer-checked:border-brand",
        "after:size-2 after:rounded-full after:bg-transparent after:transition-colors after:content-['']",
        "peer-checked:after:bg-brand"
      )}
    />
  );
}

/**
 * Whether the element scrolls horizontally and has more content past its end edge (the right in LTR, the left in RTL, where scrollLeft runs negative), kept current on scroll and resize.
 */
function useHorizontalOverflow(): {
  scrollRef: React.RefObject<HTMLDivElement | null>;
  hasMoreAtEnd: boolean;
} {
  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const [hasMoreAtEnd, setHasMoreAtEnd] = React.useState(false);

  React.useEffect(() => {
    const element = scrollRef.current;
    if (!element) return;
    const update = (): void => {
      const maxScroll = element.scrollWidth - element.clientWidth;
      setHasMoreAtEnd(maxScroll > 1 && Math.abs(element.scrollLeft) < maxScroll - 1);
    };
    update();
    element.addEventListener("scroll", update, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(update);
    observer?.observe(element);
    return () => {
      element.removeEventListener("scroll", update);
      observer?.disconnect();
    };
  }, []);

  return { scrollRef, hasMoreAtEnd };
}

function Matrix({
  elementId,
  headline,
  description,
  inputId,
  rows,
  columns,
  value = {},
  onChange,
  required = false,
  requiredLabel,
  errorMessage,
  dir = "auto",
  disabled = false,
  imageUrl,
  videoUrl,
}: Readonly<MatrixProps>): React.JSX.Element {
  const errorAria = getElementErrorAria(inputId, errorMessage);
  const { scrollRef, hasMoreAtEnd } = useHorizontalOverflow();

  // Ensure value is always an object (value already has default of {})
  const selectedValues = value;

  const handleSelect = (rowId: string, columnId: string): void => {
    onChange({ ...selectedValues, [rowId]: columnId });
  };

  const handleDeselect = (rowId: string, columnId: string): void => {
    if (selectedValues[rowId] !== columnId) {
      return;
    }
    // Create new object without the rowId property
    const { [rowId]: _, ...rest } = selectedValues;
    onChange(rest);
  };

  return (
    <div className="w-full space-y-4" id={elementId} dir={dir}>
      {/* Matrix Table. The outer group is role="group" (a set of per-row radio groups), which
          doesn't support aria-required; the visible "Required" badge conveys it. aria-invalid is
          global, so it stays. The group is named by its headline via aria-labelledby (no <legend>,
          so the headline media/badge are not nested in invalid block content). */}
      <fieldset
        className="w-full space-y-4"
        aria-labelledby={`${inputId}-headline`}
        aria-invalid={errorAria.ariaInvalid}
        aria-describedby={errorAria.ariaDescribedBy}>
        <ElementHeader
          headlineId={`${inputId}-headline`}
          headline={headline}
          description={description}
          required={required}
          requiredLabel={requiredLabel}
          imageUrl={imageUrl}
          videoUrl={videoUrl}
        />

        <div className="relative" data-element-input>
          <ElementError errorMessage={errorMessage} dir={dir} id={errorAria.errorId} />

          {/* The table fits its container: the labels wrap between words (wrap-break-word, which unlike wrap-anywhere leaves a column's minimum width at its longest word), so a column is never narrower than a whole word and the row labels take the width the columns leave. A fixed layout with a forced row-label share split words mid-word on a phone. Only when whole words cannot fit (many columns on a narrow card) does the table scroll, and then a fade at the end edge says so. */}
          <div className="relative">
            <div ref={scrollRef} className="overflow-x-auto">
              <table className="w-full border-collapse">
                {/* Column headers */}
                <thead>
                  <tr>
                    {/* Empty top-left corner: a plain data cell (not a header), so it is neither an
                      empty <th> nor announced. Each cell radio is named by its row + column header. */}
                    <td className="p-2" />
                    {columns.map((column) => {
                      const columnHeaderId = `${inputId}-col-${column.id}`;
                      return (
                        <th
                          key={column.id}
                          id={columnHeaderId}
                          className="p-1 text-center align-bottom font-normal wrap-break-word">
                          <Label className="justify-center leading-5">{column.label}</Label>
                        </th>
                      );
                    })}
                  </tr>
                </thead>
                {/* Rows */}
                <tbody>
                  {rows.map((row, index) => {
                    const rowGroupId = `${inputId}-row-${row.id}`;
                    const rowHeaderId = `${rowGroupId}-header`;
                    const selectedColumnId = selectedValues[row.id];
                    const baseBgColor = index % 2 === 0 ? "bg-input-bg" : "bg-transparent";

                    return (
                      <tr key={row.id} className={cn("relative", baseBgColor)} dir={dir}>
                        {/* Row label */}
                        <th
                          scope="row"
                          id={rowHeaderId}
                          className={cn(
                            "rounded-s-input py-2 ps-2 pe-1 text-start align-middle wrap-break-word"
                          )}>
                          <div className="flex flex-col gap-0 leading-none">
                            <Label className="leading-5">{row.label}</Label>
                          </div>
                        </th>
                        {/* Column options for this row */}
                        {columns.map((column, colIndex) => {
                          const cellId = `${rowGroupId}-${column.id}`;
                          const columnHeaderId = `${inputId}-col-${column.id}`;
                          const isLastColumn = colIndex === columns.length - 1;
                          const isSelected = selectedColumnId === column.id;

                          return (
                            <td
                              key={column.id}
                              className={cn(
                                "p-2 text-center align-middle",
                                isLastColumn && "rounded-e-input"
                              )}>
                              <label
                                htmlFor={cellId}
                                className={cn(
                                  "flex justify-center",
                                  disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer"
                                )}>
                                <input
                                  type="radio"
                                  id={cellId}
                                  name={rowGroupId}
                                  value={column.id}
                                  checked={isSelected}
                                  disabled={disabled}
                                  aria-labelledby={`${rowHeaderId} ${columnHeaderId}`}
                                  className="peer sr-only"
                                  onChange={() => {
                                    handleSelect(row.id, column.id);
                                  }}
                                  onClick={() => {
                                    // Native radios cannot be unchecked by re-clicking; allow deselect
                                    // when the field is not required.
                                    if (!required && isSelected) {
                                      handleDeselect(row.id, column.id);
                                    }
                                  }}
                                />
                                <MatrixRadioIndicator />
                              </label>
                            </td>
                          );
                        })}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {hasMoreAtEnd ? (
              <div
                aria-hidden="true"
                className="pointer-events-none absolute inset-y-0 end-0 w-8 bg-linear-to-l from-(--fb-survey-background-color) to-transparent rtl:bg-linear-to-r"
              />
            ) : null}
          </div>
        </div>
      </fieldset>
    </div>
  );
}

export { Matrix };
export type { MatrixProps };
