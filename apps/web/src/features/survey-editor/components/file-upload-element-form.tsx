import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useQuery } from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { type JSX, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import type { TSurveyElement, TSurveyFileUploadElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { organizationSettingsPath } from "../../../lib/navigation";
import { billingSummaryQuery } from "../../../lib/queries/billing";
import { workspaceQuery } from "../../../lib/queries/workspaces";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { toast } from "../../../lib/toast";
import { UNCAPPED_UPLOAD_SIZE_MB, maxUploadSizeMB } from "../../billing/lib/limits";
import { ElementFormInput } from "./element-form-input/index";
import { ValidationRulesEditor } from "./validation-rules-editor";
import { AdvancedOptionToggle } from "../../../components/ui/advanced-option-toggle";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { TransLink } from "../../../components/ui/trans-link";

interface FileUploadFormProps {
  localSurvey: TSurvey;
  element: TSurveyFileUploadElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const FileUploadElementForm = ({
  localSurvey,
  element,
  elementIdx,
  updateElement,
  isInvalid,
  locale,
}: FileUploadFormProps): JSX.Element => {
  const { t } = useTranslation();
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages);
  const [isMaxSizeError, setIsMaxSizeError] = useState(false);
  const { data: workspace } = useQuery(workspaceQuery(localSurvey.workspaceId));
  const organizationId = workspace?.organizationId;
  const { data: billing } = useQuery({
    ...billingSummaryQuery(organizationId ?? ""),
    enabled: !!organizationId,
  });
  // The Forma Cloud plan caps the size a question may allow; null where nothing caps it.
  const maxSizeInMBLimit = maxUploadSizeMB(billing);

  const handleMaxSizeInMBToggle = (checked: boolean) => {
    updateElement(elementIdx, {
      maxSizeInMB: checked ? (maxSizeInMBLimit ?? UNCAPPED_UPLOAD_SIZE_MB) : undefined,
    });
  };

  const [parent] = useAutoAnimate();

  return (
    <form>
      <ElementFormInput
        id="headline"
        value={element.headline}
        label={t("workspace.surveys.edit.question") + "*"}
        localSurvey={localSurvey}
        elementIdx={elementIdx}
        isInvalid={isInvalid}
        updateElement={updateElement}
        locale={locale}
        autoFocus={!element.headline?.default || element.headline.default.trim() === ""}
      />
      <div ref={parent}>
        {element.subheader !== undefined && (
          <div className="inline-flex w-full items-center">
            <div className="w-full">
              <ElementFormInput
                id="subheader"
                value={element.subheader}
                label={t("common.description")}
                localSurvey={localSurvey}
                elementIdx={elementIdx}
                isInvalid={isInvalid}
                updateElement={updateElement}
                locale={locale}
                autoFocus={!element.subheader?.default || element.subheader.default.trim() === ""}
              />
            </div>
          </div>
        )}
        {element.subheader === undefined && (
          <Button
            size="sm"
            className="mt-3"
            variant="secondary"
            type="button"
            onClick={() => {
              updateElement(elementIdx, {
                subheader: createI18nString("", surveyLanguageCodes),
              });
            }}>
            <PlusIcon className="mr-1 size-4" />
            {t("workspace.surveys.edit.add_description")}
          </Button>
        )}
      </div>
      <div className="mt-6 space-y-6">
        <AdvancedOptionToggle
          isChecked={element.allowMultipleFiles}
          onToggle={() => updateElement(elementIdx, { allowMultipleFiles: !element.allowMultipleFiles })}
          htmlId="allowMultipleFile"
          title={t("workspace.surveys.edit.allow_multiple_files")}
          description={t("workspace.surveys.edit.let_people_upload_up_to_25_files_at_the_same_time")}
          childBorder
          customContainerClass="p-0"></AdvancedOptionToggle>

        <AdvancedOptionToggle
          isChecked={!!element.maxSizeInMB}
          onToggle={handleMaxSizeInMBToggle}
          htmlId="maxFileSize"
          title={t("workspace.surveys.edit.max_file_size")}
          description={t("workspace.surveys.edit.limit_the_maximum_file_size")}
          childBorder
          customContainerClass="p-0">
          <label htmlFor="autoCompleteResponses" className="cursor-pointer bg-slate-50 p-4">
            <p className="text-sm font-semibold text-slate-700">
              <Trans
                i18nKey="workspace.surveys.edit.limit_upload_file_size_to_mb"
                components={{
                  fileSizeInput: (
                    <Input
                      autoFocus
                      type="number"
                      id="fileSizeLimit"
                      value={element.maxSizeInMB}
                      onChange={(e) => {
                        const parsedValue = Number.parseInt(e.target.value, 10);
                        if (maxSizeInMBLimit !== null && parsedValue > maxSizeInMBLimit) {
                          toast.error(
                            t("workspace.surveys.edit.max_file_size_limit_is_mb", {
                              maxSize: maxSizeInMBLimit,
                            })
                          );
                          setIsMaxSizeError(true);
                          updateElement(elementIdx, { maxSizeInMB: maxSizeInMBLimit });
                          return;
                        }
                        updateElement(elementIdx, { maxSizeInMB: parsedValue });
                      }}
                      className="mr-2 ml-2 inline w-20 bg-white text-center text-sm"
                    />
                  ),
                }}
              />
            </p>
            {isMaxSizeError && organizationId && (
              <p className="text-xs text-red-500">
                <Trans
                  i18nKey="workspace.surveys.edit.max_file_size_limit_is_mb_upgrade"
                  values={{ maxSize: maxSizeInMBLimit }}
                  components={{
                    upgradeLink: (
                      <TransLink
                        className="underline"
                        target="_blank"
                        rel="noopener noreferrer"
                        href={organizationSettingsPath(organizationId, "billing")}
                      />
                    ),
                  }}
                />
              </p>
            )}
          </label>
        </AdvancedOptionToggle>
      </div>

      <ValidationRulesEditor
        elementType={element.type}
        validation={element.validation}
        onUpdateValidation={(validation) => {
          updateElement(elementIdx, {
            validation,
          });
        }}
        element={element}
      />
    </form>
  );
};
