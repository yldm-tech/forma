/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { downloadResponsesFile } from "./utils";

describe("downloadResponsesFile", () => {
  let captured: File[];

  beforeEach(() => {
    captured = [];
    // jsdom implements neither of these, and the object URL itself is irrelevant here — we only need the File the helper built.
    URL.createObjectURL = vi.fn((file: Blob) => {
      captured.push(file as File);
      return "blob:mock";
    });
    URL.revokeObjectURL = vi.fn();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("prefixes the csv with a UTF-8 BOM so Excel does not decode it as ANSI", async () => {
    downloadResponsesFile("responses.csv", "headline\n日本語の回答\n", "csv");

    expect(captured).toHaveLength(1);
    // Assert on the bytes rather than Blob.text(): the UTF-8 decode that text() performs strips a leading BOM, which is the byte under test.
    const bytes = new Uint8Array(await captured[0].arrayBuffer());
    expect(Array.from(bytes.slice(0, 3))).toEqual([0xef, 0xbb, 0xbf]);
    // text() runs a UTF-8 decode, which drops the leading BOM, so this asserts the payload behind it is untouched.
    expect(await captured[0].text()).toBe("headline\n日本語の回答\n");
  });

  test("leaves the xlsx bytes untouched", async () => {
    const xlsxBytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04]);
    downloadResponsesFile("responses.xlsx", btoa(String.fromCharCode(...xlsxBytes)), "xlsx");

    expect(captured).toHaveLength(1);
    const bytes = new Uint8Array(await captured[0].arrayBuffer());
    expect(Array.from(bytes)).toEqual(Array.from(xlsxBytes));
  });
});
