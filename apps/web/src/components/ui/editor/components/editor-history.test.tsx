import { LexicalComposer } from "@lexical/react/LexicalComposer";
import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import { ContentEditable } from "@lexical/react/LexicalContentEditable";
import { LexicalErrorBoundary } from "@lexical/react/LexicalErrorBoundary";
import { RichTextPlugin } from "@lexical/react/LexicalRichTextPlugin";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import {
  $createParagraphNode,
  $createTextNode,
  $getRoot,
  type LexicalEditor,
  REDO_COMMAND,
  UNDO_COMMAND,
} from "lexical";
import { useEffect, useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { EditorHistoryPlugin } from "./editor";
import { ToolbarPlugin } from "./toolbar-plugin";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const CaptureEditor = ({ onEditor }: { onEditor: (editor: LexicalEditor) => void }) => {
  const [editor] = useLexicalComposerContext();
  useEffect(() => onEditor(editor), [editor, onEditor]);
  return null;
};

/** A rich-text field as the survey editor mounts one: seeded from the stored text on first render, and reloaded whenever the caller flips updateTemplate (a template or another text swapped in). */
const Field = ({
  stored,
  updateTemplate,
  setText,
  onEditor,
}: {
  stored: string;
  updateTemplate: boolean;
  setText: (text: string) => void;
  onEditor: (editor: LexicalEditor) => void;
}) => {
  const [firstRender, setFirstRender] = useState(true);
  return (
    <LexicalComposer
      initialConfig={{
        namespace: "test",
        onError: (error: Error) => {
          throw error;
        },
      }}>
      <ToolbarPlugin
        getText={() => stored}
        setText={setText}
        firstRender={firstRender}
        setFirstRender={setFirstRender}
        updateTemplate={updateTemplate}
        excludedToolbarItems={["blockType"]}
        setShowRecallItemSelect={() => {}}
        setShowFallbackInput={() => {}}
        setShowLinkEditor={() => {}}
      />
      <RichTextPlugin
        contentEditable={<ContentEditable aria-label="Question" />}
        ErrorBoundary={LexicalErrorBoundary}
      />
      <EditorHistoryPlugin />
      <CaptureEditor onEditor={onEditor} />
    </LexicalComposer>
  );
};

const fieldText = () => screen.getByRole("textbox", { name: "Question" }).textContent;

const type = async (editor: LexicalEditor | undefined, text: string) => {
  await act(async () => {
    editor?.update(() => {
      $getRoot().append($createParagraphNode().append($createTextNode(text)));
    });
  });
};

const dispatch = async (editor: LexicalEditor | undefined, command: typeof UNDO_COMMAND) => {
  await act(async () => {
    editor?.dispatchCommand(command, undefined);
  });
};

const renderField = (stored: string, setText = vi.fn()) => {
  let editor: LexicalEditor | undefined;
  const onEditor = (captured: LexicalEditor) => {
    editor = captured;
  };
  const view = render(
    <I18nextProvider i18n={i18n}>
      <Field stored={stored} updateTemplate={false} setText={setText} onEditor={onEditor} />
    </I18nextProvider>
  );
  const reload = (next: string, updateTemplate: boolean) =>
    view.rerender(
      <I18nextProvider i18n={i18n}>
        <Field stored={next} updateTemplate={updateTemplate} setText={setText} onEditor={onEditor} />
      </I18nextProvider>
    );
  return { editor: () => editor, reload };
};

describe("EditorHistoryPlugin", () => {
  test("Undo takes back an edit and stops at the loaded text; Redo brings the edit back and saves it", async () => {
    const setText = vi.fn();
    const field = renderField("<p>What should we build next?</p>", setText);
    await waitFor(() => expect(fieldText()).toBe("What should we build next?"));

    await type(field.editor(), "Tell us why.");
    await waitFor(() => expect(fieldText()).toBe("What should we build next?Tell us why."));

    await dispatch(field.editor(), UNDO_COMMAND);
    await waitFor(() => expect(fieldText()).toBe("What should we build next?"));
    // The undone text is what gets saved, not the edit it replaced.
    expect(setText.mock.lastCall?.[0]).not.toContain("Tell us why.");

    // The loaded text is the first step: undoing again does not empty the field.
    await dispatch(field.editor(), UNDO_COMMAND);
    expect(fieldText()).toBe("What should we build next?");

    await dispatch(field.editor(), REDO_COMMAND);
    await waitFor(() => expect(fieldText()).toBe("What should we build next?Tell us why."));
    expect(setText.mock.lastCall?.[0]).toContain("Tell us why.");
  });

  test("text loaded over an edit starts a new history, so Undo cannot bring back the text it replaced", async () => {
    const field = renderField("<p>How did we do?</p>");
    await waitFor(() => expect(fieldText()).toBe("How did we do?"));
    await type(field.editor(), "Any details?");
    await waitFor(() => expect(fieldText()).toBe("How did we do?Any details?"));

    field.reload("<p>いかがでしたか?</p>", true);
    await waitFor(() => expect(fieldText()).toBe("いかがでしたか?"));

    await dispatch(field.editor(), UNDO_COMMAND);
    expect(fieldText()).toBe("いかがでしたか?");

    await type(field.editor(), "詳しく教えてください。");
    await dispatch(field.editor(), UNDO_COMMAND);
    await waitFor(() => expect(fieldText()).toBe("いかがでしたか?"));
  });
});
