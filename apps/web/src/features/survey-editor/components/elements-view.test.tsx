import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useEffect, useState } from "react";
import { I18nextProvider } from "react-i18next";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TWorkspace } from "@forma/types/workspace";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { ElementsView } from "./elements-view";

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const ELEMENT_ID = "q1";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no ResizeObserver; the display-type switch of the multiple-choice form only needs to construct one.
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(cleanup);

const surveyWith = (element: TSurveyElement): TSurvey =>
  ({
    id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e",
    name: "Feedback",
    type: "link",
    workspaceId: WORKSPACE_ID,
    status: "draft",
    welcomeCard: { enabled: false, timeToFinish: true, showResponseCount: false },
    questions: [],
    blocks: [{ id: "blockone0000000000000000", name: "Block 1", elements: [element] }],
    endings: [],
    hiddenFields: { enabled: true, fieldIds: [] },
    variables: [],
    followUps: [],
    publishOn: null,
    languages: [],
    metadata: {},
  }) as unknown as TSurvey;

type TLabelled = { label: { default: string } };

const choice = (id: string) => ({ id, label: { default: "" } });

const multipleChoice = {
  id: ELEMENT_ID,
  type: TSurveyElementTypeEnum.MultipleChoiceSingle,
  headline: { default: "What do you use most?" },
  required: true,
  shuffleOption: "none",
  choices: [choice("choiceone000000000000000"), choice("choicetwo000000000000000")],
} as TSurveyElement;

const ranking = {
  id: ELEMENT_ID,
  type: TSurveyElementTypeEnum.Ranking,
  headline: { default: "Rank these" },
  required: true,
  shuffleOption: "none",
  choices: [choice("choiceone000000000000000"), choice("choicetwo000000000000000")],
} as TSurveyElement;

const matrix = {
  id: ELEMENT_ID,
  type: TSurveyElementTypeEnum.Matrix,
  headline: { default: "Rate each" },
  required: true,
  shuffleOption: "none",
  rows: [choice("rowone000000000000000000"), choice("rowtwo000000000000000000")],
  columns: [choice("colone000000000000000000"), choice("coltwo000000000000000000")],
} as TSurveyElement;

/** The Questions tab of the editor over a survey with one question, opened, holding the survey in state as the editor does. */
const renderEditor = (element: TSurveyElement) => {
  let latest: TSurvey = surveyWith(element);
  const Editor = () => {
    const [survey, setSurvey] = useState(latest);
    useEffect(() => {
      latest = survey;
    }, [survey]);
    return (
      <ElementsView
        localSurvey={survey}
        setLocalSurvey={setSurvey}
        activeElementId={ELEMENT_ID}
        setActiveElementId={() => {}}
        workspace={{ id: WORKSPACE_ID } as TWorkspace}
        invalidElements={null}
        setInvalidElements={() => {}}
        selectedLanguageCode="default"
        isCxMode
        locale="en-US"
        responseCount={0}
        quotas={[]}
      />
    );
  };
  const { container } = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient()}>
        <Editor />
      </QueryClientProvider>
    </I18nextProvider>
  );
  const input = (id: string) => {
    const found = container.querySelector<HTMLInputElement>(`input[id="${id}"]`);
    if (!found) throw new Error(`no input ${id}`);
    return found;
  };
  // The default-language labels of one of the element's lists (choices, rows, columns) as the editor holds them.
  const labels = (list: string) => {
    const current = latest.blocks[0]?.elements[0] as unknown as Record<string, TLabelled[]>;
    return (current[list] ?? []).map((item) => item.label.default);
  };
  return { input, labels };
};

/** Types text one character at a time with no pause, as fast typing reaches the field: nothing has been committed when the next key arrives. */
const typeInto = (input: HTMLInputElement, text: string) => {
  for (let end = 1; end <= text.length; end++) {
    fireEvent.change(input, { target: { value: text.slice(0, end) } });
  }
};

// Longer than the 100 ms after which a field commits what was typed, so any commit still pending has landed.
const afterCommits = () => act(() => new Promise((resolve) => setTimeout(resolve, 300)));

describe.each([
  { name: "multiple choice", element: multipleChoice, list: "choices", prefix: "choice" },
  { name: "ranking", element: ranking, list: "choices", prefix: "choice" },
  { name: "matrix rows", element: matrix, list: "rows", prefix: "row" },
  { name: "matrix columns", element: matrix, list: "columns", prefix: "column" },
])("$name", ({ element, list, prefix }) => {
  // CJ-02: Enter well inside the 100 ms commit delay added the next row, and the label's commit then landed with the list as it was before that row and removed it, with the focus in it.
  test("Enter right after the last keystroke keeps every label typed and the focus on the new row", async () => {
    const { input, labels } = renderEditor(element);

    const first = input(`${prefix}-1`);
    first.focus();
    typeInto(first, "Reporting");
    fireEvent.keyDown(first, { key: "Enter" });
    await waitFor(() => expect(document.activeElement?.id).toBe(`${prefix}-2`));

    const second = input(`${prefix}-2`);
    typeInto(second, "Integrations");
    fireEvent.keyDown(second, { key: "Enter" });
    await waitFor(() => expect(document.activeElement?.id).toBe(`${prefix}-3`));

    await afterCommits();
    expect(labels(list)).toEqual(["", "Reporting", "Integrations", ""]);
    expect(document.activeElement?.id).toBe(`${prefix}-3`);
  });

  // Two fields typed in quick succession each committed its label onto the list as it was before either, so the second commit erased the first.
  test("typing into one row and at once into the next keeps both labels", async () => {
    const { input, labels } = renderEditor(element);

    const first = input(`${prefix}-0`);
    first.focus();
    typeInto(first, "Reporting");
    const second = input(`${prefix}-1`);
    second.focus();
    typeInto(second, "Integrations");

    await afterCommits();
    expect(labels(list)).toEqual(["Reporting", "Integrations"]);
  });
});
