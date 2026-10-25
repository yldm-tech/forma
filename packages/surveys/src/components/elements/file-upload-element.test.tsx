import { cleanup, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import type { TSurveyFileUploadElement } from "@forma/types/surveys/elements";
import { FileUploadElement } from "./file-upload-element";

// vitestSetup's useTranslation returns the bare key, so these assertions read which message was chosen.

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  document.body.innerHTML = "";
});

const element = (overrides: Partial<TSurveyFileUploadElement> = {}) =>
  ({
    id: "upload",
    type: TSurveyElementTypeEnum.FileUpload,
    headline: { default: "Attach a file" },
    required: false,
    allowMultipleFiles: false,
    ...overrides,
  }) as TSurveyFileUploadElement;

const renderElement = (el: TSurveyFileUploadElement, onFileUpload = vi.fn()) =>
  render(
    <FileUploadElement
      element={el}
      value={[]}
      onChange={vi.fn()}
      onFileUpload={onFileUpload}
      surveyId="survey-1"
      languageCode="default"
      ttc={{}}
      setTtc={vi.fn()}
      autoFocusEnabled={false}
      currentElementId="upload"
    />
  );

const stubPointer = (coarse: boolean) => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({ matches: coarse && query === "(pointer: coarse)", media: query }))
  );
};

describe("FileUploadElement", () => {
  test("says what it accepts before a file is picked", () => {
    renderElement(element({ allowedFileExtensions: ["png", "jpg", "pdf"], maxSizeInMB: 10 }));
    expect(screen.getByText("file_upload.allowed_hint")).toBeTruthy();
  });

  test("lists the formats alone when there is no size limit", () => {
    renderElement(element({ allowedFileExtensions: ["png", "jpg"] }));
    expect(screen.getByText("PNG, JPG")).toBeTruthy();
  });

  test("states the size limit alone when any format is allowed", () => {
    renderElement(element({ maxSizeInMB: 5 }));
    expect(screen.getByText("file_upload.max_size_hint")).toBeTruthy();
  });

  test("shows no hint without restrictions", () => {
    const { container } = renderElement(element());
    expect(container.querySelector("label[data-fb-focus-ring]")?.textContent).toBe(
      "errors.file_input.placeholder_text"
    );
  });

  test("asks for a tap, not a drag, on a touch screen", () => {
    stubPointer(true);
    renderElement(element());
    expect(screen.getByText("file_upload.tap_to_upload")).toBeTruthy();
    expect(screen.queryByText("errors.file_input.placeholder_text")).toBeNull();
  });

  test("keeps click or drag with a mouse", () => {
    stubPointer(false);
    renderElement(element());
    expect(screen.getByText("errors.file_input.placeholder_text")).toBeTruthy();
  });

  test("a rejected file type names the allowed formats instead of uploading", async () => {
    const onFileUpload = vi.fn();
    const { container } = renderElement(element({ allowedFileExtensions: ["png", "pdf"] }), onFileUpload);
    const input = container.querySelector("input[type=file]") as HTMLInputElement;
    // happy-dom does not deliver fireEvent.change's target.files to a file input, so the selection is set and dispatched directly.
    Object.defineProperty(input, "files", {
      configurable: true,
      value: [new File(["x"], "anim.gif", { type: "image/gif" })],
    });
    input.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => {
      expect(screen.getByText("file_upload.invalid_type")).toBeTruthy();
    });
    expect(onFileUpload).not.toHaveBeenCalled();
  });
});
