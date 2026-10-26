import type { RowSelectionState, Updater } from "@tanstack/react-table";
import { useMemo, useState } from "react";

/**
 * The response table's row selection, which "select all N matching" can widen to every response matching the view's filter. All-matching holds only for the filter it was chosen under: a change of filter ends it for good and unticks the rows, so going back to that filter does not bring it back.
 */
export const useResponseSelection = (criteriaKey: string, loadedRows: { responseId: string }[]) => {
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const [allMatchingFor, setAllMatchingFor] = useState<string | null>(null);
  // Adjusted while rendering rather than in an effect, so the old filter's selection never shows under the new one.
  if (allMatchingFor !== null && allMatchingFor !== criteriaKey) {
    setAllMatchingFor(null);
    setRowSelection({});
  }
  const allMatching = allMatchingFor === criteriaKey;

  // Every loaded row reads as ticked while all matching responses are selected, rows loaded after the choice included.
  const allRowsSelected = useMemo<RowSelectionState>(
    () => Object.fromEntries(loadedRows.map((row) => [row.responseId, true])),
    [loadedRows]
  );
  const effectiveRowSelection = allMatching ? allRowsSelected : rowSelection;

  return {
    rowSelection: effectiveRowSelection,
    allMatching,
    /** Ticking or unticking a row narrows "all matching" back to the rows. */
    onRowSelectionChange: (updater: Updater<RowSelectionState>) => {
      setRowSelection(typeof updater === "function" ? updater(effectiveRowSelection) : updater);
      setAllMatchingFor(null);
    },
    selectAllMatching: () => setAllMatchingFor(criteriaKey),
    clear: () => {
      setRowSelection({});
      setAllMatchingFor(null);
    },
  };
};
