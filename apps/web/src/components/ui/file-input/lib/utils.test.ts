import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { toast } from "../../../../lib/toast";
import { checkForYoutubePrivacyMode, getAllowedFiles } from "./utils";

// The converter's browser entry decodes with WebAssembly and encodes on a canvas; the test replaces it at that boundary.
const convert = vi.hoisted(() => vi.fn());
vi.mock("heic-convert/browser", () => ({ default: convert }));

const sized = (name: string, size: number, type = "") => {
  const file = new File(["x"], name, { type });
  Object.defineProperty(file, "size", { value: size });
  return file;
};

const toastMessages = () => toast.getSnapshot().map((shown) => shown.message);

afterEach(() => {
  convert.mockReset();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

describe("getAllowedFiles", () => {
  test("converts a HEIC image to a JPEG at quality 0.9", async () => {
    convert.mockResolvedValue(new Uint8Array([0xff, 0xd8, 0xff]));
    const heic = sized("photo.heic", 2000, "image/heic");

    const [converted, ...rest] = await getAllowedFiles([heic], ["png", "heic"], 5);

    expect(rest).toEqual([]);
    expect(converted?.name).toBe("photo.jpg");
    expect(converted?.type).toBe("image/jpeg");
    expect(new Uint8Array(await converted!.arrayBuffer())).toEqual(new Uint8Array([0xff, 0xd8, 0xff]));
    expect(convert).toHaveBeenCalledWith({ buffer: expect.any(Uint8Array), format: "JPEG", quality: 0.9 });
    expect(toastMessages()).toEqual([]);
  });

  test("checks the size again after converting", async () => {
    convert.mockResolvedValue(new Uint8Array(150_000));
    expect(await getAllowedFiles([sized("photo.HEIC", 50_000)], ["heic"], 0.1)).toEqual([]);
    expect(toastMessages()).toEqual(["File exceeds 0.1 MB size limit."]);
  });

  test("a HEIC image that cannot be decoded is an unsupported file", async () => {
    convert.mockRejectedValue(new Error("not a HEIF image"));
    expect(await getAllowedFiles([sized("broken.heic", 10)], ["heic"])).toEqual([]);
    expect(toastMessages()).toEqual(["Unsupported file type."]);
  });

  test("keeps allowed files as they are and names what it left out", async () => {
    const png = sized("a.PNG", 10);
    const allowed = await getAllowedFiles(
      [png, sized("b.gif", 10), sized("c.txt", 10), sized("d.png", 6_000_000)],
      ["png"],
      5
    );
    expect(allowed).toEqual([png]);
    expect(convert).not.toHaveBeenCalled();
    expect(toastMessages()).toEqual(["File exceeds 5 MB size limit. 2 files have unsupported types."]);
  });
});

describe("checkForYoutubePrivacyMode", () => {
  test("returns true for youtube-nocookie.com URLs", () => {
    expect(checkForYoutubePrivacyMode("https://www.youtube-nocookie.com/watch?v=test")).toBe(true);
    expect(checkForYoutubePrivacyMode("https://www.youtube-nocookie.com/embed/123")).toBe(true);
  });

  test("returns false for regular youtube.com and other URLs", () => {
    expect(checkForYoutubePrivacyMode("https://www.youtube.com/watch?v=test")).toBe(false);
    expect(checkForYoutubePrivacyMode("https://www.example.com")).toBe(false);
  });

  test("returns false for invalid, empty or non-string values", () => {
    expect(checkForYoutubePrivacyMode("not-a-url")).toBe(false);
    expect(checkForYoutubePrivacyMode("")).toBe(false);
    expect(checkForYoutubePrivacyMode("   ")).toBe(false);
    expect(checkForYoutubePrivacyMode(null as unknown as string)).toBe(false);
    expect(checkForYoutubePrivacyMode(123 as unknown as string)).toBe(false);
  });
});
