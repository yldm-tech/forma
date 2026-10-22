import { createId } from "@paralleldrive/cuid2";
import { PlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import type { TWorkspace as Workspace } from "@forma/types/workspace";
import { TSurveyBlock } from "@forma/types/surveys/blocks";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { addMultiLanguageLabels, extractLanguageCodes } from "../../../lib/survey/i18n";
import { addElementToBlock } from "../lib/blocks";
import { scrollElementCardIntoView } from "../lib/utils";
import { getElementDefaults, getGroupedElementTypes, universalElementPresets } from "../lib/elements";
import { Button } from "../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";

interface AddElementToBlockButtonProps {
  localSurvey: TSurvey;
  block: TSurveyBlock;
  setLocalSurvey: (survey: TSurvey) => void;
  setActiveElementId: (elementId: string) => void;
  workspace: Workspace;
  isCxMode: boolean;
}

export const AddElementToBlockButton = ({
  localSurvey,
  block,
  setLocalSurvey,
  setActiveElementId,
  workspace,
  isCxMode,
}: AddElementToBlockButtonProps) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const groupedElementTypes = getGroupedElementTypes(t, isCxMode);

  const handleAddElement = (elementType: string) => {
    // Get language symbols and add multi-language support
    const languageSymbols = extractLanguageCodes(localSurvey.languages);

    const elementDefaults = getElementDefaults(elementType, workspace, t);
    const elementWithLabels = addMultiLanguageLabels(
      {
        ...universalElementPresets,
        ...elementDefaults,
        id: createId(),
        type: elementType,
      } as TSurveyElement,
      languageSymbols
    );

    const result = addElementToBlock(localSurvey, block.id, elementWithLabels);

    if (!result.ok) {
      toast.error(result.error.message);
      setOpen(false);
      return;
    }

    setLocalSurvey(result.data);
    setOpen(false);
    setActiveElementId(elementWithLabels.id);
    scrollElementCardIntoView(elementWithLabels.id);
  };

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button variant="secondary">
          <PlusIcon className="size-4" />
          <div>
            <p className="text-sm font-medium text-slate-900">
              {t("workspace.surveys.edit.add_question_to_block")}
            </p>
          </div>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        {groupedElementTypes.map((group, index) => (
          <div key={group.category.id}>
            {index > 0 && <DropdownMenuSeparator />}
            <DropdownMenuLabel className="pt-2 text-xs font-semibold tracking-wide text-slate-500 uppercase">
              {group.category.label}
            </DropdownMenuLabel>
            {group.elements.map((elementType) => (
              <DropdownMenuItem
                key={elementType.id}
                className="min-h-8"
                onClick={() => handleAddElement(elementType.id)}>
                <elementType.icon className="size-4" />
                <span className="ml-2">{elementType.label}</span>
              </DropdownMenuItem>
            ))}
          </div>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
