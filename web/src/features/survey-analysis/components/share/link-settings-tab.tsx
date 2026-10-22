import { getLanguageLabel } from "@forma/i18n-utils/utils";
import type { TI18nString } from "@forma/types/i18n";
import type { TSurvey, TSurveyMetadata } from "@forma/types/surveys/types";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { FileInput } from "../../../../components/ui/file-input";
import { Input } from "../../../../components/ui/input";
import { Label } from "../../../../components/ui/label";
import { Select } from "../../../../components/ui/select";
import { createI18nString, extractLanguageCodes, getEnabledLanguages } from "../../../../lib/survey/i18n";
import { toast } from "../../../../lib/toast";
import { useUpdateSurveySettings } from "./use-survey-settings";

interface LinkSettings {
  title: TI18nString;
  description: TI18nString;
  ogImage: string;
}

/** forma2's LinkSettingsTab: the title, description and preview image a shared link unfurls with, per survey language. */
export const LinkSettingsTab = ({
  survey,
  isReadOnly,
  locale,
}: {
  survey: TSurvey;
  isReadOnly: boolean;
  locale: string;
}) => {
  const { t } = useTranslation();
  const updateSurvey = useUpdateSurveySettings(survey);
  const enabledLanguages = getEnabledLanguages(survey.languages);
  const hasMultipleLanguages = enabledLanguages.length > 1;
  const defaultLanguageCode = hasMultipleLanguages
    ? (survey.languages.find((lang) => lang.default)?.language.code ?? "default")
    : "default";
  const languageCodes = useMemo(() => extractLanguageCodes(survey.languages), [survey.languages]);
  const [selectedLanguageCode, setSelectedLanguageCode] = useState(defaultLanguageCode);
  const currentLangCode = selectedLanguageCode === defaultLanguageCode ? "default" : selectedLanguageCode;

  const initial = useMemo<LinkSettings>(
    () => ({
      title: survey.metadata?.title ?? createI18nString("", languageCodes),
      description: survey.metadata?.description ?? createI18nString("", languageCodes),
      ogImage: survey.metadata?.ogImage ?? "",
    }),
    [survey.metadata, languageCodes]
  );
  const [saved, setSaved] = useState(initial);
  const [values, setValues] = useState(initial);
  const isDirty = JSON.stringify(values) !== JSON.stringify(saved);

  const onSubmit = async () => {
    if (updateSurvey.isPending || isReadOnly) return;
    const current = survey.metadata ?? {
      title: createI18nString("", languageCodes),
      description: createI18nString("", languageCodes),
    };
    const metadata: TSurveyMetadata = {
      title: { ...current.title, ...values.title },
      description: { ...current.description, ...values.description },
      ogImage: values.ogImage || undefined,
    };
    try {
      await updateSurvey.mutateAsync({ metadata });
      toast.success(t("workspace.surveys.edit.settings_saved_successfully"));
      setSaved(values);
    } catch {
      toast.error(t("common.something_went_wrong_please_try_again"));
    }
  };

  const textFields: {
    name: "title" | "description";
    label: string;
    description: string;
    placeholder: string;
  }[] = [
    {
      name: "title",
      label: t("workspace.surveys.share.link_settings.link_title"),
      description: t("workspace.surveys.share.link_settings.link_title_description"),
      placeholder: survey.name,
    },
    {
      name: "description",
      label: t("workspace.surveys.share.link_settings.link_description"),
      description: t("workspace.surveys.share.link_settings.link_description_description"),
      placeholder: "Please complete this survey.",
    },
  ];

  return (
    <div className="px-1">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void onSubmit();
        }}
        className="space-y-6">
        {hasMultipleLanguages && (
          <div className="space-y-2">
            <Label htmlFor="link-settings-language">{t("common.language")}</Label>
            <Select
              id="link-settings-language"
              value={selectedLanguageCode}
              onChange={(event) => setSelectedLanguageCode(event.target.value)}>
              {enabledLanguages.map((lang) => (
                <option key={lang.language.code} value={lang.language.code}>
                  {getLanguageLabel(lang.language.code, locale)}
                </option>
              ))}
            </Select>
            <p className="text-sm text-slate-500">
              {t("workspace.surveys.share.link_settings.language_help_text")}
            </p>
          </div>
        )}

        {textFields.map((field) => (
          <div key={field.name} className="space-y-2">
            <Label htmlFor={`link-settings-${field.name}`}>{field.label}</Label>
            <Input
              id={`link-settings-${field.name}`}
              value={values[field.name][currentLangCode] ?? ""}
              className="bg-white"
              onChange={(e) =>
                setValues((current) => ({
                  ...current,
                  [field.name]: { ...current[field.name], [currentLangCode]: e.target.value },
                }))
              }
              placeholder={field.placeholder}
              disabled={isReadOnly}
            />
            <p className="text-sm text-slate-500">{field.description}</p>
          </div>
        ))}

        <div className="space-y-2">
          <Label>{t("workspace.surveys.share.link_settings.preview_image")}</Label>
          <FileInput
            id={`og-image-upload-${survey.id}`}
            onFileUpload={(urls) => setValues((current) => ({ ...current, ogImage: urls?.[0] ?? "" }))}
            fileUrl={values.ogImage || undefined}
            disabled={isReadOnly}
          />
          <p className="text-sm text-slate-500">
            {t("workspace.surveys.share.link_settings.preview_image_description")}
          </p>
        </div>

        <Button type="submit" disabled={updateSurvey.isPending || isReadOnly || !isDirty}>
          {updateSurvey.isPending ? t("common.saving") : t("common.save")}
        </Button>
      </form>
    </div>
  );
};
