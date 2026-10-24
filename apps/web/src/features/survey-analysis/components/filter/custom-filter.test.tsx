import type { TFunction } from "i18next";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { toast } from "../../../../lib/toast";
import { analysisApi } from "../../api";
import { downloadAttachments } from "./custom-filter";

const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";

// Renders the key and its values, so a test sees which message was chosen and with what.
const t = ((key: string, values?: Record<string, unknown>) =>
  values ? `${key} ${JSON.stringify(values)}` : key) as unknown as TFunction;

const counts = (overrides: Partial<Awaited<ReturnType<typeof analysisApi.attachmentCounts>>> = {}) => ({
  fileCount: 3,
  responseCount: 2,
  exceedsMaxFiles: false,
  maxFiles: 5000,
  ...overrides,
});

/** The toasts shown, the newest last. */
const shown = () => toast.getSnapshot().map(({ kind, message }) => ({ kind, message }));

afterEach(() => {
  for (const { id } of toast.getSnapshot()) toast.dismiss(id);
  vi.restoreAllMocks();
});

// forma2's handleDownloadAttachments: the dry run decides, and only a download that can go ahead navigates.
describe("downloadAttachments", () => {
  test("an export over the limit is refused with the limit, without navigating", async () => {
    const dryRun = vi
      .spyOn(analysisApi, "attachmentCounts")
      .mockResolvedValue(counts({ exceedsMaxFiles: true }));
    const navigate = vi.fn();
    await downloadAttachments(SURVEY_ID, undefined, t, navigate);
    expect(dryRun).toHaveBeenCalledWith(SURVEY_ID, undefined);
    expect(navigate).not.toHaveBeenCalled();
    expect(shown()).toEqual([
      {
        kind: "error",
        message: 'workspace.surveys.responses.too_many_attachments_to_download {"maxFiles":5000}',
      },
    ]);
  });

  test("an export without files is refused", async () => {
    vi.spyOn(analysisApi, "attachmentCounts").mockResolvedValue(counts({ fileCount: 0 }));
    const navigate = vi.fn();
    await downloadAttachments(SURVEY_ID, undefined, t, navigate);
    expect(navigate).not.toHaveBeenCalled();
    expect(shown()).toEqual([
      { kind: "error", message: "workspace.surveys.responses.no_attachments_to_download" },
    ]);
  });

  test("a failed dry run is reported", async () => {
    vi.spyOn(analysisApi, "attachmentCounts").mockRejectedValue(new Error("offline"));
    const navigate = vi.fn();
    await downloadAttachments(SURVEY_ID, undefined, t, navigate);
    expect(navigate).not.toHaveBeenCalled();
    expect(shown()).toEqual([
      { kind: "error", message: "workspace.surveys.responses.error_downloading_attachments" },
    ]);
  });

  test("all attachments: navigates to the export without filters", async () => {
    vi.spyOn(analysisApi, "attachmentCounts").mockResolvedValue(counts());
    const navigate = vi.fn();
    await downloadAttachments(SURVEY_ID, undefined, t, navigate);
    expect(navigate).toHaveBeenCalledWith(`/api/surveys/${SURVEY_ID}/attachments?`);
    expect(shown()).toEqual([
      {
        kind: "success",
        message: 'workspace.surveys.responses.attachments_download_started {"fileCount":3}',
      },
    ]);
  });

  test("filtered attachments: the dry run and the download carry the filter", async () => {
    const criteria = { finished: true, tags: { applied: ["vip"] } };
    const dryRun = vi.spyOn(analysisApi, "attachmentCounts").mockResolvedValue(counts());
    const navigate = vi.fn();
    await downloadAttachments(SURVEY_ID, criteria, t, navigate);
    expect(dryRun).toHaveBeenCalledWith(SURVEY_ID, criteria);
    const url = new URL(navigate.mock.calls[0]?.[0] as string, "https://app.example.com");
    expect(url.pathname).toBe(`/api/surveys/${SURVEY_ID}/attachments`);
    expect(url.searchParams.get("dryRun")).toBeNull();
    expect(JSON.parse(url.searchParams.get("filters") ?? "")).toEqual(criteria);
  });
});

// The dry run asks the export route itself, with dryRun=true, and reads the counts out of the v3 envelope.
test("attachmentCounts asks the export route for a dry run", async () => {
  const fetch = vi.fn(
    async (_path: string) =>
      new Response(JSON.stringify({ data: counts() }), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
  );
  vi.stubGlobal("fetch", fetch);
  try {
    await expect(analysisApi.attachmentCounts(SURVEY_ID, { finished: true })).resolves.toEqual(counts());
    const url = new URL(fetch.mock.calls[0]?.[0] as string, "https://app.example.com");
    expect(url.pathname).toBe(`/api/surveys/${SURVEY_ID}/attachments`);
    expect(url.searchParams.get("dryRun")).toBe("true");
    expect(JSON.parse(url.searchParams.get("filters") ?? "")).toEqual({ finished: true });
  } finally {
    vi.unstubAllGlobals();
  }
});
