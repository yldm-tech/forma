import { useAutoAnimate } from "@formkit/auto-animate/react";
import { PlusIcon } from "lucide-react";
import { type JSX, useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { TSurveyContactInfoElement, TSurveyElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { ElementFormInput } from "./element-form-input/index";
import { ValidationRulesEditor } from "./validation-rules-editor";
import { Button } from "../../../components/ui/button";
import { ElementToggleTable } from "../../../components/ui/element-toggle-table";

interface ContactInfoElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyContactInfoElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  lastElement: boolean;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const ContactInfoElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  locale,
}: ContactInfoElementFormProps): JSX.Element => {
  const { t } = useTranslation();
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages ?? []);

  const fields = [
    {
      id: "firstName",
      label: t("workspace.surveys.edit.first_name"),
      ...element.firstName,
    },
    {
      id: "lastName",
      label: t("workspace.surveys.edit.last_name"),
      ...element.lastName,
    },
    {
      id: "email",
      label: t("common.email"),
      ...element.email,
    },
    {
      id: "phone",
      label: t("common.phone"),
      ...element.phone,
    },
    {
      id: "company",
      label: t("workspace.surveys.edit.company"),
      ...element.company,
    },
  ];

  useEffect(() => {
    const allFieldsAreOptional = [
      element.firstName,
      element.lastName,
      element.email,
      element.phone,
      element.company,
    ]
      .filter((field) => field.show)
      .every((field) => !field.required);

    updateElement(elementIdx, { required: !allFieldsAreOptional });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sync-only effect; adding updateElement/elementIdx would loop as it writes back into the survey
  }, [element.firstName, element.lastName, element.email, element.phone, element.company]);

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
          type="contact"
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
      />
    </form>
  );
};
