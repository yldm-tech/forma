import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import {
  $createParagraphNode,
  $createTextNode,
  $getRoot,
  $getSelection,
  $isElementNode,
  $setSelection,
  type LexicalEditor,
  REDO_COMMAND,
  UNDO_COMMAND,
} from "lexical";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { EDITOR_SEED_TAG } from "../lib/seed-tag";
import { Editor } from "./editor";
import { RecallNode } from "./recall-node";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const openText = (id: string, headline: string) =>
  ({
    id,
    type: TSurveyElementTypeEnum.OpenText,
    headline: { default: headline },
    required: true,
    inputType: "text",
    charLimit: { enabled: false },
  }) as unknown as TSurveyElement;

const surveyWith = (headline: string): TSurvey =>
  ({
    id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e",
    name: "Feedback",
    type: "link",
    status: "draft",
    welcomeCard: { enabled: false, timeToFinish: true, showResponseCount: false },
    blocks: [
      {
        id: "blockone0000000000000000",
        name: "Block 1",
        elements: [openText("q1", "What is your name?"), openText("q2", headline)],
      },
    ],
    endings: [],
    hiddenFields: { enabled: true, fieldIds: [] },
    variables: [],
    followUps: [],
    languages: [],
    metadata: {},
  }) as unknown as TSurvey;

/** The headline of question q2 as the survey editor mounts it: the full Editor with RecallPlugin, and every edit written back into the survey, so the survey object the plugins see changes after each edit. */
const Headline = ({ stored }: { stored: string }) => {
  const [survey, setSurvey] = useState(() => surveyWith(stored));
  const [firstRender, setFirstRender] = useState(true);
  return (
    <I18nextProvider i18n={i18n}>
      <span id="headline-label">Question</span>
      <Editor
        id="headline"
        ariaLabelledBy="headline-label"
        excludedToolbarItems={["blockType"]}
        disableLists
        getText={() => stored}
        setText={(text) => setSurvey(surveyWith(text))}
        firstRender={firstRender}
        setFirstRender={setFirstRender}
        localSurvey={survey}
        elementId="q2"
        selectedLanguageCode="default"
      />
    </I18nextProvider>
  );
};

const textbox = () => screen.getByRole("textbox", { name: "Question" });

const editorOf = (element: HTMLElement) =>
  (element as HTMLElement & { __lexicalEditor: LexicalEditor }).__lexicalEditor;

const dispatch = async (editor: LexicalEditor, command: typeof UNDO_COMMAND) => {
  await act(async () => {
    editor.dispatchCommand(command, undefined);
  });
};

describe("Editor history in a survey field", () => {
  test("Redo still works after undoing back to an empty field", async () => {
    render(<Headline stored="" />);
    const editor = editorOf(textbox());
    // Let the load commit first; an update made while it is pending would be part of it.
    await act(async () => {});

    // The author clicks into the field (a selection, no edit), then types.
    await act(async () => {
      editor.update(() => {
        $getRoot().selectEnd();
      });
    });
    await act(async () => {
      editor.update(() => {
        $getRoot().append($createParagraphNode().append($createTextNode("You said ")));
      });
    });
    await waitFor(() => expect(textbox().textContent).toBe("You said "));

    await dispatch(editor, UNDO_COMMAND);
    await waitFor(() => expect(textbox().textContent).toBe(""));

    // The write-back of the undo re-runs RecallPlugin's recall conversion, which is tagged as a load. In the browser that update commits because the selection is dirty (editor.focus() marks it so), giving a load-tagged update that changes no node ("focus" and "forma-seed" tags, no dirty node); this replays it.
    await act(async () => {
      editor.update(
        () => {
          const selection = $getSelection();
          if (selection) $setSelection(selection.clone());
        },
        { tag: EDITOR_SEED_TAG }
      );
    });

    await dispatch(editor, REDO_COMMAND);
    await waitFor(() => expect(textbox().textContent).toBe("You said "));
  });

  test("an empty field is not an undo step Lexical would refuse to restore", async () => {
    render(<Headline stored="" />);
    const editor = editorOf(textbox());
    await act(async () => {});

    // Typed into before any selection was made, so the loaded state is empty: no node and no selection.
    await act(async () => {
      editor.update(() => {
        $getRoot().append($createParagraphNode().append($createTextNode("You said ")));
      });
    });
    await waitFor(() => expect(textbox().textContent).toBe("You said "));

    await dispatch(editor, UNDO_COMMAND);
    expect(textbox().textContent).toBe("You said ");
  });

  test("Undo bringing a recalled item back does not reopen the fallback popover and take the caret", async () => {
    render(<Headline stored="<p>You said #recall:q1/fallback:friend# </p>" />);
    const editor = editorOf(textbox());
    await waitFor(() => expect(textbox().textContent).toContain("What is your name?"));

    // The author opens the fallback popover, then goes back to the text and deletes the recalled item: the popover goes with it.
    fireEvent.click(screen.getByRole("button", { name: "Edit Recall" }));
    expect(await screen.findByPlaceholderText("Enter fallback value")).toBeTruthy();
    await act(async () => {
      editor.update(() => {
        const paragraph = $getRoot().getFirstChildOrThrow();
        if (!$isElementNode(paragraph)) throw new Error("expected a paragraph");
        for (const node of paragraph.getChildren()) if (node instanceof RecallNode) node.remove();
      });
    });
    await waitFor(() => expect(textbox().textContent).not.toContain("What is your name?"));
    expect(screen.queryByPlaceholderText("Enter fallback value")).toBeNull();

    await dispatch(editor, UNDO_COMMAND);
    await waitFor(() => expect(textbox().textContent).toContain("What is your name?"));

    // The caret stays in the editor, so the next Cmd/Ctrl+Z is the editor's and not the browser's own undo.
    expect(screen.queryByPlaceholderText("Enter fallback value")).toBeNull();
    expect(document.activeElement?.tagName).not.toBe("INPUT");
  });
});
