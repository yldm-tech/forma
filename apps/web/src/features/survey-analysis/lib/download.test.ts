/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { exportFileName, saveExport } from "./download";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("exportFileName", () => {
  test("keeps the server's name when it carries the extension", () => {
    expect(exportFileName("export-survey-2026-01-01-00-00-00-utc.csv", "csv")).toBe(
      "export-survey-2026-01-01-00-00-00-utc.csv"
    );
  });

  test("forces the format's extension", () => {
    expect(exportFileName("export", "xlsx")).toBe("export.xlsx");
    expect(exportFileName("export.csv", "xlsx")).toBe("export.csv.xlsx");
  });

  test("falls back to a dated name without one", () => {
    expect(exportFileName(null, "csv", new Date("2026-09-19T10:00:00Z"))).toBe("responses-2026-09-19.csv");
    expect(exportFileName("  ", "xlsx", new Date("2026-09-19T10:00:00Z"))).toBe("responses-2026-09-19.xlsx");
    expect(exportFileName(null, "csv", new Date("2026-09-19T10:00:00Z"), "contacts")).toBe(
      "contacts-2026-09-19.csv"
    );
  });
});

describe("saveExport", () => {
  test("saves the bytes as they came, the server's BOM included, under the export's name", async () => {
    const saved: Blob[] = [];
    URL.createObjectURL = vi.fn((blob: Blob) => {
      saved.push(blob);
      return "blob:export";
    });
    URL.revokeObjectURL = vi.fn();
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    const blob = new Blob(["﻿", "headline\n日本語\n"], { type: "text/csv" });

    saveExport({ blob, filename: "export-a.csv" }, "csv");

    expect(click).toHaveBeenCalledTimes(1);
    const anchor = click.mock.contexts[0] as HTMLAnchorElement;
    expect(anchor.download).toBe("export-a.csv");
    const bytes = new Uint8Array(await saved[0]!.arrayBuffer());
    expect(Array.from(bytes.slice(0, 3))).toEqual([0xef, 0xbb, 0xbf]);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:export");
  });
});
