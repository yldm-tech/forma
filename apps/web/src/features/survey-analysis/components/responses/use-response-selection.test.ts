/**
 * @vitest-environment jsdom
 */
import { act, renderHook } from "@testing-library/react";
import { describe, expect, test } from "vite-plus/test";
import { useResponseSelection } from "./use-response-selection";

const ALL = JSON.stringify({});
const KEYWORD = JSON.stringify({ data: { keyword: "a" } });
const ROWS = [{ responseId: "r1" }, { responseId: "r2" }, { responseId: "r3" }];
const KEYWORD_ROWS = [{ responseId: "r1" }];

const renderSelection = () =>
  renderHook(({ criteriaKey, rows }) => useResponseSelection(criteriaKey, rows), {
    initialProps: { criteriaKey: ALL, rows: ROWS },
  });

describe("useResponseSelection", () => {
  test("select all matching ticks every loaded row, rows loaded later included", () => {
    const { result, rerender } = renderSelection();
    act(() => result.current.selectAllMatching());
    expect(result.current.allMatching).toBe(true);
    expect(result.current.rowSelection).toEqual({ r1: true, r2: true, r3: true });
    rerender({ criteriaKey: ALL, rows: [...ROWS, { responseId: "r4" }] });
    expect(result.current.rowSelection).toEqual({ r1: true, r2: true, r3: true, r4: true });
  });

  test("unticking a row narrows all matching back to the rows", () => {
    const { result } = renderSelection();
    act(() => result.current.selectAllMatching());
    act(() => result.current.onRowSelectionChange((current) => ({ ...current, r2: false })));
    expect(result.current.allMatching).toBe(false);
    expect(result.current.rowSelection).toEqual({ r1: true, r2: false, r3: true });
  });

  test("a change of filter ends all matching and unticks its rows", () => {
    const { result, rerender } = renderSelection();
    act(() => result.current.selectAllMatching());
    rerender({ criteriaKey: KEYWORD, rows: KEYWORD_ROWS });
    expect(result.current.allMatching).toBe(false);
    expect(result.current.rowSelection).toEqual({});
  });

  test("going back to the filter does not bring all matching back", () => {
    const { result, rerender } = renderSelection();
    act(() => result.current.selectAllMatching());
    rerender({ criteriaKey: KEYWORD, rows: KEYWORD_ROWS });
    rerender({ criteriaKey: ALL, rows: ROWS });
    expect(result.current.allMatching).toBe(false);
    expect(result.current.rowSelection).toEqual({});
  });

  test("clear drops the ticked rows and all matching", () => {
    const { result } = renderSelection();
    act(() => result.current.onRowSelectionChange({ r1: true }));
    expect(result.current.rowSelection).toEqual({ r1: true });
    act(() => result.current.selectAllMatching());
    act(() => result.current.clear());
    expect(result.current.allMatching).toBe(false);
    expect(result.current.rowSelection).toEqual({});
  });
});
