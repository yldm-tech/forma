import { useAutoAnimate } from "@formkit/auto-animate/react";
import { PlusIcon } from "lucide-react";
import { type JSX, useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { TSurveyAddressElement, TSurveyElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { ElementFormInput } from "./element-form-input/index";
import { ValidationRulesEditor } from "./validation-rules-editor";
import { Button } from "../../../components/ui/button";
import { ElementToggleTable } from "../../../components/ui/element-toggle-table";

interface AddressElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyAddressElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const AddressElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  locale,
}: AddressElementFormProps): JSX.Element => {
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages ?? []);
  const { t } = useTranslation();
  const fields = [
    {
      id: "addressLine1",
      label: t("workspace.surveys.edit.address_line_1"),
      ...element.addressLine1,
    },
    {
      id: "addressLine2",
      label: t("workspace.surveys.edit.address_line_2"),
      ...element.addressLine2,
    },
    {
      id: "city",
      label: t("workspace.surveys.edit.city"),
      ...element.city,
    },
    {
      id: "state",
      label: t("workspace.surveys.edit.state"),
      ...element.state,
    },
    {
      id: "zip",
      label: t("workspace.surveys.edit.zip"),
      ...element.zip,
    },
    {
      id: "country",
      label: t("workspace.surveys.edit.country"),
      ...element.country,
    },
  ];

  useEffect(() => {
    const allFieldsAreOptional = [
      element.addressLine1,
      element.addressLine2,
      element.city,
      element.state,
      element.zip,
      element.country,
    ]
      .filter((field) => field.show)
      .every((field) => !field.required);

    updateElement(elementIdx, { required: !allFieldsAreOptional });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sync-only effect; adding updateElement/elementIdx would loop as it writes back into the survey
  }, [element.addressLine1, element.addressLine2, element.city, element.state, element.zip, element.country]);

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
            variant="secondary"
            className="mt-4"
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

        <ElementToggleTable
          type="address"
          fields={fields}
          localSurvey={localSurvey}
          elementIdx={elementIdx}
          isInvalid={isInvalid}
          updateElement={updateElement}
          locale={locale}
        />
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
