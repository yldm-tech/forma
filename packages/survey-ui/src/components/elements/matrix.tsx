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
 *
 * `ringed` makes the dot carry the focus ring. The table needs that, because its cell-sized label is not the right shape to ring (globals.css suppresses the label ring inside a table). A stacked option is a row like a single-select option, so its label carries the ring, and ringing the dot as well would draw two.
 */
function MatrixRadioIndicator({ ringed }: Readonly<{ ringed: boolean }>): React.JSX.Element {
  return (
    <span
      aria-hidden="true"
      data-fb-focus-ring={ringed ? "" : undefined}
      className={cn(
        "border-control-border relative flex size-4 shrink-0 items-center justify-center rounded-full border bg-white shadow-xs transition-colors",
        "peer-checked:border-brand",
        "after:size-2 after:rounded-full after:bg-transparent after:transition-colors after:content-['']",
        "peer-checked:after:bg-brand"
      )}
    />
  );
}

type MatrixLayout = "table" | "stacked";

/**
 * Picks the matrix layout for the width the card gives it.
 *
 * The table is the default: its labels wrap between words, so a column is never narrower than its longest word. When even that does not fit (four or five columns on a phone), the table used to scroll sideways behind a fade, and a respondent could answer every row without ever seeing the last columns, which on a satisfaction scale are the best ones. Then the matrix stacks instead: one group per row, with every column listed under it.
 *
 * The table's overflowing scroll width is its minimum width, which depends on the labels and not on the card, so the hook records it and only goes back to the table once the card is at least that wide again. That threshold is what keeps the layout from flipping back and forth when stacking makes the page taller and a scrollbar takes width away. A change to the labels (a language switch) starts over from the table and measures again.
 *
 * Measured in a layout effect, before paint, so a phone never shows the table first.
 */
function useMatrixLayout(contentKey: string): {
  containerRef: React.RefObject<HTMLDivElement | null>;
  layout: MatrixLayout;
} {
  const containerRef = React.useRef<HTMLDivElement | null>(null);
  // The content the stacked layout was chosen for, with the table width it did not fit in.
  const [stackedFor, setStackedFor] = React.useState<{ contentKey: string; tableWidth: number } | null>(null);
  const layout: MatrixLayout = stackedFor?.contentKey === contentKey ? "stacked" : "table";
  const tableWidth = stackedFor?.tableWidth ?? 0;
  // The id of the radio that had focus when a resize switched the layout. Switching replaces every radio, which drops focus to the page; both layouts render the same radio ids, so the new one is found by id and focused again.
  const focusedRadioIdRef = React.useRef<string | null>(null);

  React.useLayoutEffect(() => {
    const id = focusedRadioIdRef.current;
    focusedRadioIdRef.current = null;
    if (!id) return;
    containerRef.current?.ownerDocument.getElementById(id)?.focus({ preventScroll: true });
  }, [layout]);

  React.useLayoutEffect(() => {
    const element = containerRef.current;
    if (!element) return;
    const switchLayout = (next: { contentKey: string; tableWidth: number } | null): void => {
      const active = element.ownerDocument.activeElement;
      focusedRadioIdRef.current = active && active.id && element.contains(active) ? active.id : null;
      setStackedFor(next);
    };
    const update = (): void => {
      if (layout === "table") {
        // A hidden card measures 0 wide and fits; it is measured again when it is shown.
        if (element.scrollWidth - element.clientWidth > 1) {
          switchLayout({ contentKey, tableWidth: element.scrollWidth });
        }
      } else if (element.clientWidth >= tableWidth) {
        switchLayout(null);
      }
    };
    update();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(update);
    observer?.observe(element);
    return () => {
      observer?.disconnect();
    };
  }, [layout, contentKey, tableWidth]);

  return { containerRef, layout };
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
  const contentKey = JSON.stringify([rows.map((row) => row.label), columns.map((column) => column.label)]);
  const { containerRef, layout } = useMatrixLayout(contentKey);

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

  // The native radio for one cell. Both layouts render the same id, name and value, so the answer and the form data do not depend on the layout.
  const renderRadio = (row: MatrixOption, column: MatrixOption, labelledBy?: string): React.JSX.Element => {
    const rowGroupId = `${inputId}-row-${row.id}`;
    const isSelected = selectedValues[row.id] === column.id;
    return (
      <input
        type="radio"
        id={`${rowGroupId}-${column.id}`}
        name={rowGroupId}
        value={column.id}
        checked={isSelected}
        disabled={disabled}
        aria-labelledby={labelledBy}
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
    );
  };

  const cursorClass = disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer";

  const table = (
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
          const baseBgColor = index % 2 === 0 ? "bg-input-bg" : "bg-transparent";

          return (
            <tr key={row.id} className={cn("relative", baseBgColor)} dir={dir}>
              {/* Row label */}
              <th
                scope="row"
                id={rowHeaderId}
                className={cn("rounded-s-input py-2 ps-2 pe-1 text-start align-middle wrap-break-word")}>
                <div className="flex flex-col gap-0 leading-none">
                  <Label className="leading-5">{row.label}</Label>
                </div>
              </th>
              {/* Column options for this row */}
              {columns.map((column, colIndex) => {
                const columnHeaderId = `${inputId}-col-${column.id}`;
                const isLastColumn = colIndex === columns.length - 1;

                return (
                  <td
                    key={column.id}
                    className={cn(
                      "relative p-0 text-center align-middle",
                      isLastColumn && "rounded-e-input"
                    )}>
                    {/* The whole cell is the target: the label is at least 44px tall and 32px wide (the width the 16px dot and p-2 gave before, so the table needs no more room than it did and a scale that fitted a phone still fits), and its ::before covers the rest of the cell when a long row label makes the row taller. */}
                    <label
                      htmlFor={`${rowGroupId}-${column.id}`}
                      className={cn(
                        "flex min-h-11 w-full min-w-8 items-center justify-center",
                        "before:absolute before:inset-0 before:content-['']",
                        cursorClass
                      )}>
                      {renderRadio(row, column, `${rowHeaderId} ${columnHeaderId}`)}
                      <MatrixRadioIndicator ringed />
                    </label>
                  </td>
                );
              })}
            </tr>
          );
        })}
      </tbody>
    </table>
  );

  // One group per row, its columns listed underneath, each option a full-width target. Used when the table does not fit the card (see useMatrixLayout).
  const stacked = (
    <div className="space-y-2">
      {rows.map((row, index) => {
        const rowGroupId = `${inputId}-row-${row.id}`;
        const rowHeaderId = `${rowGroupId}-header`;
        const baseBgColor = index % 2 === 0 ? "bg-input-bg" : "bg-transparent";

        return (
          <fieldset
            key={row.id}
            aria-labelledby={rowHeaderId}
            className={cn("rounded-input min-w-0 p-2", baseBgColor)}
            dir={dir}>
            <Label id={rowHeaderId} className="px-1 pb-1 leading-5 wrap-break-word">
              {row.label}
            </Label>
            <div className="flex flex-col">
              {columns.map((column) => (
                <label
                  key={column.id}
                  htmlFor={`${rowGroupId}-${column.id}`}
                  className={cn(
                    "rounded-input flex min-h-11 w-full items-center gap-3 px-1 text-start",
                    cursorClass
                  )}>
                  {renderRadio(row, column)}
                  <MatrixRadioIndicator ringed={false} />
                  <Label className="leading-5 wrap-break-word">{column.label}</Label>
                </label>
              ))}
            </div>
          </fieldset>
        );
      })}
    </div>
  );

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

          {/* The table fits its container: the labels wrap between words (wrap-break-word, which unlike wrap-anywhere leaves a column's minimum width at its longest word), so a column is never narrower than a whole word and the row labels take the width the columns leave. A fixed layout with a forced row-label share split words mid-word on a phone. When whole words cannot fit (many columns on a narrow card), the rows stack instead. The table layout keeps overflow-x-auto as the fallback for a browser without ResizeObserver whose card narrows after the first measurement. */}
          <div
            ref={containerRef}
            data-matrix-layout={layout}
            className={layout === "table" ? "overflow-x-auto" : undefined}>
            {layout === "table" ? table : stacked}
          </div>
        </div>
      </fieldset>
    </div>
  );
}

export { Matrix };
export type { MatrixProps };
