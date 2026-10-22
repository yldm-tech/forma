import { normalizeLanguageCode } from "@forma/i18n-utils/canonical";
import { type TIso639Language, supportedLanguages } from "@forma/i18n-utils/utils";
import { useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { ChevronDown, InfoIcon, PlusIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { ConfirmationModal } from "../../components/ui/confirmation-modal";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Popover, PopoverContent, PopoverTrigger } from "../../components/ui/popover";
import { Select } from "../../components/ui/select";
import { errorMessage } from "../../lib/errors";
import { meQuery } from "../../lib/queries/auth";
import {
  type WorkspaceDetail,
  createLanguage,
  deleteLanguage,
  languageSurveysQuery,
  updateLanguage,
  updateWorkspace,
  workspaceKeys,
} from "../../lib/queries/workspaces";
import { toast } from "../../lib/toast";
import {
  type LanguageRowValue,
  NEW_LANGUAGE_ID,
  defaultLanguageOptions,
  isDefaultSurveyLanguage,
  languagesError,
} from "../../lib/workspace-languages";

/** forma2's survey languages page. */
export const WorkspaceLanguagesPage = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  return (
    <PageContentWrapper width="settings">
      <PageHeader pageTitle={t("common.survey_languages")} />
      <SettingsCard
        title={t("workspace.languages.multi_language_surveys")}
        description={t("workspace.languages.multi_language_surveys_description")}>
        <EditLanguages
          // The form restarts from what the server stored whenever the stored languages or default change.
          key={JSON.stringify([workspace.languages, workspace.config.defaultSurveyLanguage ?? null])}
          workspace={workspace}
          locale={me.locale}
          isReadOnly={workspace.access.isReadOnly}
        />
      </SettingsCard>
    </PageContentWrapper>
  );
};

const storedRows = (workspace: WorkspaceDetail): LanguageRowValue[] =>
  workspace.languages.map(({ id, code, alias }) => ({ id, code, alias }));

const storedDefault = (workspace: WorkspaceDetail) =>
  normalizeLanguageCode(workspace.config.defaultSurveyLanguage ?? "") ?? "";

interface Confirmation {
  open: boolean;
  text: string;
  languageId: string;
  isButtonDisabled: boolean;
}

/**
 * forma2's EditLanguage: the rows and the default survey language are one form, edited and saved together. Removals are staged until save; the save writes creations and alias changes, then the default when it changed, then the removals, because the server checks a removal against the stored default.
 */
const EditLanguages = ({
  workspace,
  locale,
  isReadOnly,
}: {
  workspace: WorkspaceDetail;
  locale: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [languages, setLanguages] = useState(() => storedRows(workspace));
  const [isEditing, setIsEditing] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [defaultLanguage, setDefaultLanguage] = useState(() => storedDefault(workspace));
  const [deletedIds, setDeletedIds] = useState<string[]>([]);
  const [confirmation, setConfirmation] = useState<Confirmation>({
    open: false,
    text: "",
    languageId: "",
    isButtonDisabled: false,
  });
  const options = useMemo(() => defaultLanguageOptions(languages, locale), [languages, locale]);

  const refresh = () => queryClient.invalidateQueries({ queryKey: workspaceKeys.detail(workspace.id) });

  const requestRemoval = async (languageId: string) => {
    const language = languages.find((row) => row.id === languageId);
    if (language && isDefaultSurveyLanguage(language.code, defaultLanguage)) {
      setConfirmation({
        open: true,
        languageId,
        text: t("workspace.languages.cannot_remove_default_survey_language_warning"),
        isButtonDisabled: true,
      });
      return;
    }
    try {
      const surveys = await queryClient.fetchQuery(languageSurveysQuery(workspace.id, languageId));
      setConfirmation(
        surveys.length > 0
          ? {
              open: true,
              languageId,
              text: `${t("workspace.languages.cannot_remove_language_warning")}:\n\n${surveys.map((name) => `• ${name}`).join("\n")}\n\n${t("workspace.languages.remove_language_from_surveys_to_remove_it_from_workspace")}`,
              isButtonDisabled: true,
            }
          : {
              open: true,
              languageId,
              text: t("workspace.languages.delete_language_confirmation"),
              isButtonDisabled: false,
            }
      );
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const stageRemoval = (languageId: string) => {
    setLanguages((rows) => rows.filter((row) => row.id !== languageId));
    setDeletedIds((ids) => (ids.includes(languageId) ? ids : [...ids, languageId]));
    setConfirmation((current) => ({ ...current, open: false }));
  };

  const cancel = () => {
    setLanguages(storedRows(workspace));
    setDefaultLanguage(storedDefault(workspace));
    setDeletedIds([]);
    setIsEditing(false);
  };

  const save = async () => {
    const problem = languagesError(languages);
    if (problem) {
      toast.error(t(problem));
      return;
    }
    setIsSaving(true);
    try {
      await Promise.all(
        languages.map((language) =>
          language.id === NEW_LANGUAGE_ID
            ? createLanguage(workspace.id, { code: language.code, alias: language.alias })
            : updateLanguage(workspace.id, language.id, language.alias)
        )
      );
      if (defaultLanguage !== storedDefault(workspace)) {
        // Only the changed key: the server merges it onto the stored config.
        await updateWorkspace(workspace.id, { config: { defaultSurveyLanguage: defaultLanguage || null } });
      }
      if (deletedIds.length > 0) {
        try {
          await Promise.all(deletedIds.map((id) => deleteLanguage(workspace.id, id)));
        } catch (error) {
          toast.error(errorMessage(t, error));
          // The rows that survived come back from the server rather than staying hidden.
          setDeletedIds([]);
          await refresh();
          return;
        }
      }
      toast.success(
        deletedIds.length > 0
          ? t("workspace.languages.language_deleted_successfully")
          : t("workspace.languages.languages_updated_successfully")
      );
      setDeletedIds([]);
      setIsEditing(false);
      await refresh();
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-y-4 pb-4">
      <div className="space-y-4">
        {languages.length > 0 ? (
          <>
            <div className="mb-2 grid w-full grid-cols-4 gap-4">
              <span className="text-sm font-medium text-slate-800">{t("workspace.languages.language")}</span>
              <span className="text-sm font-medium text-slate-800">
                {t("workspace.languages.identifier")}
              </span>
              <span className="flex items-center gap-x-2 text-sm font-medium text-slate-800">
                {t("workspace.languages.alias")}
                <InfoIcon
                  className="size-4 text-slate-400"
                  aria-label={t("workspace.languages.alias_tooltip")}
                  role="img">
                  <title>{t("workspace.languages.alias_tooltip")}</title>
                </InfoIcon>
              </span>
            </div>
            {languages.map((language, index) => (
              <div key={language.id} className="my-3 grid grid-cols-4 gap-4">
                <LanguageSelect
                  disabled={language.id !== NEW_LANGUAGE_ID}
                  code={language.code}
                  locale={locale}
                  onSelect={(code) =>
                    setLanguages((rows) => rows.map((row, i) => (i === index ? { ...row, code } : row)))
                  }
                />
                <Input disabled value={language.code} aria-label={t("workspace.languages.identifier")} />
                <Input
                  disabled={!isEditing}
                  aria-label={t("workspace.languages.alias")}
                  onChange={(e) =>
                    setLanguages((rows) =>
                      rows.map((row, i) => (i === index ? { ...row, alias: e.target.value } : row))
                    )
                  }
                  placeholder="e.g. en_us"
                  value={language.alias ?? ""}
                />
                {language.id !== NEW_LANGUAGE_ID && isEditing ? (
                  <Button
                    className="w-fit"
                    onClick={() => void requestRemoval(language.id)}
                    size="sm"
                    variant="destructive">
                    {t("common.remove")}
                  </Button>
                ) : null}
              </div>
            ))}
          </>
        ) : (
          <p className="text-sm text-slate-500 italic">{t("workspace.languages.no_language_found")}</p>
        )}
        {isEditing && !languages.some((row) => row.id === NEW_LANGUAGE_ID) && (
          <Button
            onClick={() => setLanguages((rows) => [...rows, { id: NEW_LANGUAGE_ID, code: "", alias: "" }])}
            size="sm"
            variant="secondary">
            <PlusIcon />
            {t("workspace.languages.add_language")}
          </Button>
        )}
        {options.length > 0 && (
          <div className="flex w-full max-w-sm flex-col gap-y-2 pt-2">
            <Label htmlFor="defaultSurveyLanguage">{t("workspace.languages.default_survey_language")}</Label>
            <Select
              id="defaultSurveyLanguage"
              aria-describedby="defaultSurveyLanguage-description"
              disabled={!isEditing}
              value={defaultLanguage}
              onChange={(e) => setDefaultLanguage(e.target.value)}>
              <option value="" disabled>
                {t("workspace.languages.default_survey_language_placeholder")}
              </option>
              {options.map(({ code, label, isSelectable }) => (
                <option key={code} value={code} disabled={!isSelectable}>
                  {isSelectable
                    ? label
                    : `${label} (${t("workspace.languages.default_survey_language_unsupported")})`}
                </option>
              ))}
            </Select>
            <p id="defaultSurveyLanguage-description" className="text-sm text-slate-500">
              {t("workspace.languages.default_survey_language_description")}
            </p>
          </div>
        )}
      </div>
      {isEditing ? (
        <div className="flex gap-4">
          <Button onClick={() => void save()} size="sm" loading={isSaving} disabled={isReadOnly}>
            {t("common.save_changes")}
          </Button>
          <Button onClick={cancel} size="sm" variant="ghost" disabled={isReadOnly || isSaving}>
            {t("common.cancel")}
          </Button>
        </div>
      ) : (
        <Button className="w-fit" onClick={() => setIsEditing(true)} size="sm" disabled={isReadOnly}>
          {t("workspace.languages.edit_languages")}
        </Button>
      )}
      {isReadOnly && (
        <Alert variant="warning" className="mt-4" role="status">
          <AlertDescription>
            {t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")}
          </AlertDescription>
        </Alert>
      )}
      <ConfirmationModal
        title={t("workspace.languages.remove_language")}
        buttonText={t("workspace.languages.remove_language")}
        isButtonDisabled={confirmation.isButtonDisabled}
        onConfirm={() => stageRemoval(confirmation.languageId)}
        open={confirmation.open}
        setOpen={(open) => setConfirmation((current) => ({ ...current, open }))}
        body={confirmation.text}
      />
    </div>
  );
};

const languageLabel = (language: TIso639Language, locale: string) =>
  language.label[locale as keyof TIso639Language["label"]] ?? language.label["en-US"];

/** forma2's LanguageSelect: a searchable list of the canonical languages, fixed once a row is stored. */
const LanguageSelect = ({
  code,
  disabled,
  locale,
  onSelect,
}: {
  code: string;
  disabled: boolean;
  locale: string;
  onSelect: (code: string) => void;
}) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const canonical = normalizeLanguageCode(code) ?? code;
  const selected = supportedLanguages.find((language) => language.code === canonical);
  const items = supportedLanguages.filter((language) =>
    languageLabel(language, locale).toLowerCase().includes(search.toLowerCase())
  );
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          disabled={disabled}
          aria-label={t("workspace.languages.language")}
          className="flex h-full w-full justify-between border border-slate-200 px-3 py-2">
          <span className="mr-2 min-w-0 truncate">
            {selected ? languageLabel(selected, locale) : t("common.select")}
          </span>
          <ChevronDown className="size-4 shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="space-y-1 p-1">
        <Input
          autoComplete="off"
          aria-label={t("workspace.languages.search_items")}
          placeholder={t("workspace.languages.search_items")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <div className="max-h-96 overflow-auto">
          {items.map((language) => (
            <button
              type="button"
              key={language.code}
              className="block w-full cursor-pointer rounded-md px-4 py-2 text-left text-sm text-slate-700 hover:bg-slate-100"
              onClick={() => {
                onSelect(language.code);
                setOpen(false);
              }}>
              {languageLabel(language, locale)}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
};
