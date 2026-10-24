import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import type { TSurveyBlock } from "@forma/types/surveys/blocks";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import { BlockConditional } from "./block-conditional";

// The catalogue's ICU plurals are formatted by the bundle's i18next-icu; here a key and its count are enough to see what was announced.
vi.mock("@/lib/i18n", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/i18n")>();
  return {
    ...actual,
    getTranslations: () => (key: string, options?: { count?: number }) =>
      options?.count === undefined ? key : `${key}:${String(options.count)}`,
  };
});

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

const openText = (id: string, headline: string) => ({
  id,
  type: TSurveyElementTypeEnum.OpenText,
  headline: { default: headline },
  required: true,
  inputType: "text",
  charLimit: { enabled: false },
});

const block = {
  id: "block-1",
  name: "Block 1",
  elements: [openText("feedback", "What do you think?"), openText("improve", "What should we improve?")],
} as unknown as TSurveyBlock;

const renderBlock = (onSubmit = vi.fn()) =>
  render(
    <BlockConditional
      block={block}
      value={{}}
      onChange={vi.fn()}
      onSubmit={onSubmit}
      onBack={vi.fn()}
      onFileUpload={vi.fn()}
      isFirstBlock
      isLastBlock={false}
      languageCode="default"
      ttc={{}}
      setTtc={vi.fn()}
      surveyId="survey-1"
      autoFocusEnabled={false}
      shouldFocusOnMount={false}
      isBackButtonHidden={false}
      isAutoProgressingEnabled={false}
      fullSizeCards={false}
      surveyLanguages={[]}
    />
  );

describe("BlockConditional", () => {
  // Every element's error used to be its own live region, so a failed submit queued the same phrase once per question.
  test("a failed submit announces one summary, and the element messages are not live", async () => {
    const onSubmit = vi.fn();
    const { container } = renderBlock(onSubmit);
    fireEvent.click(screen.getByRole("button", { name: /next/i }));

    await waitFor(() => {
      expect(screen.getByRole("status").textContent).toBe("errors.x_questions_need_an_answer:2");
    });
    const liveRegions = container.querySelectorAll("[aria-live]");
    expect(liveRegions).toHaveLength(1);
    expect(liveRegions[0]?.getAttribute("role")).toBe("status");
    expect(container.querySelectorAll('[aria-invalid="true"]')).toHaveLength(2);
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
