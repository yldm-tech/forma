import type { TSurveyElement } from "@forma/types/surveys/elements";
import type { TWorkspace } from "@forma/types/workspace";
import { getLocalizedValue } from "./i18n";

/** Puts the workspace's name in place of the $[workspaceName] placeholder of an element preset's headline and subheader. */
export const replaceElementPresetPlaceholders = <T extends Partial<TSurveyElement>>(
  element: T,
  workspace: TWorkspace | undefined
): T => {
  if (!workspace) return element;
  const newElement = structuredClone(element);
  const defaultLanguageCode = "default";

  if (newElement.headline) {
    newElement.headline[defaultLanguageCode] = getLocalizedValue(
      newElement.headline,
      defaultLanguageCode
    ).replace("$[workspaceName]", workspace.name);
  }

  if (newElement.subheader) {
    newElement.subheader[defaultLanguageCode] = getLocalizedValue(
      newElement.subheader,
      defaultLanguageCode
    )?.replace("$[workspaceName]", workspace.name);
  }

  return newElement;
};
