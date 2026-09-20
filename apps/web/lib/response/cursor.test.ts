import { describe, expect, test } from "vitest";
import { Prisma } from "@forma/database/prisma";
import { applyResponseCursor, buildResponseCursorWhereClause } from "./cursor";

type TRow = { id: string; createdAt: Date };

/**
 * Evaluates the narrow subset of `ResponseWhereInput` this module emits against an in-memory row, so the assertions below are about which responses a batch would actually return rather than about the shape of an object. Anything outside that subset throws instead of quietly passing.
 */
const matches = (clause: Prisma.ResponseWhereInput, row: TRow): boolean =>
  Object.entries(clause).every(([key, value]) => {
    if (key === "OR") return (value as Prisma.ResponseWhereInput[]).some((sub) => matches(sub, row));
    if (key === "AND") return (value as Prisma.ResponseWhereInput[]).every((sub) => matches(sub, row));
    if (key === "createdAt") {
      const filter = value as { lt?: Date; equals?: Date };
      if (filter.lt !== undefined) return row.createdAt.getTime() < filter.lt.getTime();
      if (filter.equals !== undefined) return row.createdAt.getTime() === filter.equals.getTime();
      throw new Error(`unsupported createdAt filter: ${JSON.stringify(filter)}`);
    }
    if (key === "id") {
      const filter = value as { lt?: string };
      if (filter.lt !== undefined) return row.id < filter.lt;
      throw new Error(`unsupported id filter: ${JSON.stringify(filter)}`);
    }
    throw new Error(`unsupported where key: ${key}`);
  });

const day = (n: number) => new Date(Date.UTC(2026, 0, n));

// Sorted the way the batch loops order their pages: createdAt desc, then id desc. `imported` is the case
// the id-only cursor got wrong — an old createdAt carried by the management API, with a cuid minted today.
const newest: TRow = { id: "cuid-m", createdAt: day(3) };
const imported: TRow = { id: "cuid-z", createdAt: day(2) };
const sameDayOlderId: TRow = { id: "cuid-a", createdAt: day(2) };
const oldest: TRow = { id: "cuid-y", createdAt: day(1) };

const selected = (cursor: TRow, rows: TRow[]) =>
  rows.filter((row) => matches(buildResponseCursorWhereClause(cursor), row)).map((row) => row.id);

describe("buildResponseCursorWhereClause", () => {
  const rows = [newest, imported, sameDayOlderId, oldest];

  test("keeps rows whose createdAt is older than the cursor even when their id sorts above it", () => {
    expect(selected(newest, rows)).toEqual([imported.id, sameDayOlderId.id, oldest.id]);
  });

  test("breaks a createdAt tie on id, and excludes the cursor row itself", () => {
    expect(selected(imported, rows)).toEqual([sameDayOlderId.id, oldest.id]);
  });

  test("returns nothing once the last row is the cursor", () => {
    expect(selected(oldest, rows)).toEqual([]);
  });
});

describe("applyResponseCursor", () => {
  test("leaves the where clause untouched without a cursor, so the first page is unchanged", () => {
    const whereClause = { surveyId: "survey-1", AND: [{ finished: true }] };

    expect(applyResponseCursor(whereClause, undefined)).toBe(whereClause);
  });

  test("appends to the existing AND instead of replacing the filters it carries", () => {
    const filter = { finished: true };

    const result = applyResponseCursor({ surveyId: "survey-1", AND: [filter] }, imported);

    expect(result.AND).toEqual([filter, buildResponseCursorWhereClause(imported)]);
  });
});
