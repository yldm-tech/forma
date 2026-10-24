import { LexicalComposer } from "@lexical/react/LexicalComposer";
import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import { ContentEditable } from "@lexical/react/LexicalContentEditable";
import { LexicalErrorBoundary } from "@lexical/react/LexicalErrorBoundary";
import { RichTextPlugin } from "@lexical/react/LexicalRichTextPlugin";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { $createParagraphNode, $createTextNode, $getRoot, type LexicalEditor } from "lexical";
import { useEffect, useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { ToolbarPlugin } from "./toolbar-plugin";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

/** Hands the test the editor, so it can make an edit the way typing would. */
const CaptureEditor = ({ onEditor }: { onEditor: (editor: LexicalEditor) => void }) => {
  const [editor] = useLexicalComposerContext();
  useEffect(() => onEditor(editor), [editor, onEditor]);
  return null;
};

/** A rich-text field as the survey editor mounts one: seeded from the stored text on first render. */
const Field = ({
  stored,
  setText,
  onEditor,
}: {
  stored: string;
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
        excludedToolbarItems={["blockType"]}
        setShowRecallItemSelect={() => {}}
        setShowFallbackInput={() => {}}
        setShowLinkEditor={() => {}}
      />
      <RichTextPlugin
        contentEditable={<ContentEditable aria-label="Question" />}
        ErrorBoundary={LexicalErrorBoundary}
      />
      <CaptureEditor onEditor={onEditor} />
    </LexicalComposer>
  );
};

describe("ToolbarPlugin", () => {
  test("loading the stored text reports no change, and the first edit does", async () => {
    const setText = vi.fn();
    let editor: LexicalEditor | undefined;
    render(
      <I18nextProvider i18n={i18n}>
        <Field
          stored="<p>How likely are you to recommend Forma?</p>"
          setText={setText}
          onEditor={(captured) => {
            editor = captured;
          }}
        />
      </I18nextProvider>
    );

    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: "Question" }).textContent).toBe(
        "How likely are you to recommend Forma?"
      )
    );
    expect(setText).not.toHaveBeenCalled();

    await act(async () => {
      editor?.update(() => {
        $getRoot().append($createParagraphNode().append($createTextNode("Why?")));
      });
    });

    await waitFor(() => expect(setText).toHaveBeenCalledTimes(1));
    expect(setText.mock.calls[0]?.[0]).toContain("Why?");
  });
});
