import { useMemo, useTransition } from "react";
import type { Dispatch, SetStateAction } from "react";
import type { TI18nString } from "@forma/types/i18n";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import type {
  TSurvey,
  TSurveyEndScreenCard,
  TSurveyRedirectUrlCard,
  TSurveyWelcomeCard,
} from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { md } from "../../../../lib/survey/markdown-it";
import { getElementsFromBlocks } from "../../lib/client-utils";
import { Editor } from "../../../../components/ui/editor/index";

interface LocalizedEditorProps {
  id: string;
  value: TI18nString | undefined;
  localSurvey: TSurvey;
  isInvalid: boolean;
  /** Id of the visible label that names the field. */
  labelId?: string;
  /** Writes an element's field. */
  updateElement?: (elementIdx: number, data: Partial<TSurveyElement>) => void;
  /** Writes a field of the welcome card or of an ending, when isCard. */
  updateSurvey?: (
    data: Partial<TSurveyEndScreenCard> | Partial<TSurveyRedirectUrlCard> | Partial<TSurveyWelcomeCard>
  ) => void;
  selectedLanguageCode?: string;
  setSelectedLanguageCode?: (languageCode: string) => void;
  elementIdx: number;
  firstRender: boolean;
  setFirstRender?: Dispatch<SetStateAction<boolean>>;
  locale: string;
  elementId: string;
  isCard?: boolean;
  autoFocus?: boolean;
  suppressUpdates?: () => boolean;
}

export function LocalizedEditor({
  id,
  value,
  localSurvey,
  isInvalid,
  labelId,
  updateElement,
  updateSurvey,
  selectedLanguageCode = "default",
  elementIdx,
  firstRender,
  setFirstRender,
  elementId,
  isCard,
  suppressUpdates,
  autoFocus,
}: Readonly<LocalizedEditorProps>) {
  const elements = useMemo(() => getElementsFromBlocks(localSurvey.blocks), [localSurvey.blocks]);

  const [, startTransition] = useTransition();
  // The card is invalid as a whole; only the field that is actually empty is marked, so the author sees which one to fill.
  const isEmpty = value !== undefined && getTextContent(value[selectedLanguageCode] ?? "").trim() === "";

  return (
    <div className="relative w-full">
      <Editor
        id={id}
        ariaLabelledBy={labelId}
        isInvalid={isInvalid && isEmpty}
        disableLists
        excludedToolbarItems={["blockType"]}
        firstRender={firstRender}
        autoFocus={autoFocus}
        getText={() => {
          const text = value ? (value[selectedLanguageCode] ?? "") : "";
          return md.render(text);
        }}
        key={`${elementId}-${id}-${selectedLanguageCode}`}
        setFirstRender={setFirstRender}
        setText={(v: string) => {
          if (suppressUpdates?.()) {
            return;
          }

          const currentElement = elements[elementIdx];

          startTransition(() => {
            if (isCard) {
              const isWelcomeCard = elementIdx === -1;
              const isEndingCard = elementIdx >= elements.length;

              if (isEndingCard) {
                const ending = localSurvey.endings.find((ending) => ending.id === elementId);
                if ((ending as Record<string, unknown>)?.[id] === undefined) {
                  return;
                }
              }

              if (isWelcomeCard && !localSurvey.welcomeCard) {
                return;
              }

              const translatedContent = {
                ...value,
                [selectedLanguageCode]: v,
              };
              updateSurvey?.({ [id]: translatedContent });
              return;
            }

            if (
              currentElement &&
              id in currentElement &&
              (currentElement as Record<string, unknown>)[id] !== undefined
            ) {
              const translatedContent = {
                ...value,
                [selectedLanguageCode]: v,
              };
              updateElement?.(elementIdx, { [id]: translatedContent });
            }
          });
        }}
        localSurvey={localSurvey}
        elementId={elementId}
        selectedLanguageCode={selectedLanguageCode}
      />
    </div>
  );
}
