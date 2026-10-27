import type { TResponseFilterCriteria } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { CopyIcon, FilterIcon, PencilIcon, TrashIcon, TriangleAlertIcon } from "lucide-react";
import { type FormEvent, useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../../../components/ui/badge";
import { Button } from "../../../../components/ui/button";
import { Checkbox } from "../../../../components/ui/checkbox";
import { ConfirmationModal } from "../../../../components/ui/confirmation-modal";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "../../../../components/ui/dialog";
import { Input } from "../../../../components/ui/input";
import { Label } from "../../../../components/ui/label";
import { Select } from "../../../../components/ui/select";
import {
  API_KEY_EXPIRY_CHOICES,
  type ApiKeyExpiryChoice,
  LAST_CUSTOM_EXPIRY_DAY,
  apiKeyExpiry,
  firstCustomExpiryDay,
} from "../../../../lib/api-key-form";
import { ApiError } from "../../../../lib/api";
import { copyText } from "../../../../lib/clipboard";
import { errorMessage } from "../../../../lib/errors";
import { getLocalizedValue } from "../../../../lib/survey/i18n";
import { toast } from "../../../../lib/toast";
import { getElementsFromBlocks } from "../../../survey-editor/lib/client-utils";
import {
  RESULT_SHARE_SEGMENT_NOT_FOUND,
  type ResultShare,
  type Revealable,
  invalidateResultShares,
  isResultShareExpired,
  resultShareUrl,
  resultSharesApi,
  resultSharesQuery,
} from "../../result-shares";
import { useResponseFilter } from "../filter/response-filter-context";

/** The longest name of a link, as the server bounds it. */
const MAX_NAME_LENGTH = 100;

/** Whether criteria filter anything: a member left out or null filters nothing. */
export const hasCriteria = (criteria: TResponseFilterCriteria): boolean =>
  Object.values(criteria).some((value) => value !== undefined && value !== null);

/** An expiry the form offers, in the user's language: the API key form's choices, which mean the same here. */
const expiryChoiceLabel = (t: TFunction, choice: ApiKeyExpiryChoice): string => {
  switch (choice) {
    case "never":
      return t("workspace.api_keys.expiration_never");
    case "custom":
      return t("workspace.api_keys.expiration_custom");
    case "365":
      return t("workspace.api_keys.expiration_one_year");
    default:
      return t("workspace.api_keys.expiration_days", { count: Number(choice) });
  }
};

/** A day as a date input holds it (YYYY-MM-DD), in the browser's time zone. */
const inputDay = (date: Date): string => {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
};

/** What a revealable card is called in the form: its element's headline, or the hidden field's id, with what revealing it shows. */
const revealableLabel = (t: TFunction, survey: TSurvey, revealable: Revealable): string => {
  if (revealable.kind === "hiddenField") {
    return t("workspace.analysis.result_shares.reveal_hidden_field", { name: revealable.id });
  }
  const element = getElementsFromBlocks(survey.blocks).find((el) => el.id === revealable.id);
  const headline = element ? getTextContent(getLocalizedValue(element.headline, "default")) : revealable.id;
  return revealable.kind === "otherValues"
    ? t("workspace.analysis.result_shares.reveal_other_values", { headline: headline || revealable.id })
    : headline || revealable.id;
};

interface FormValues {
  name: string;
  expiry: ApiKeyExpiryChoice;
  customDay: string;
  /** The link's stored expiry, sent back as it is until the expiry field is changed: picking its day again would move it to the end of that day. */
  keptExpiresAt: string | null;
  revealed: string[];
}

const NEW_LINK: FormValues = { name: "", expiry: "never", customDay: "", keptExpiresAt: null, revealed: [] };

/** A link's settings as the form edits them: its expiry kept as it is, shown as its day, and the cards it reveals that the survey still offers (the server drops the others when the link is saved). */
const formValues = (share: ResultShare, revealable: Revealable[]): FormValues => ({
  name: share.name,
  expiry: share.expiresAt ? "custom" : "never",
  customDay: share.expiresAt ? inputDay(new Date(share.expiresAt)) : "",
  keptExpiresAt: share.expiresAt,
  revealed: share.revealedElementIds.filter((revealedId) =>
    revealable.some((item) => item.id === revealedId)
  ),
});

/** The settings of a link, new or existing: its name, when it stops working and which cards with individual answers it shows. The filter is not among them: a new link takes the summary's current one, and it never changes after. */
const ResultShareForm = ({
  survey,
  revealable,
  initial,
  submitLabel,
  filterNote,
  onSubmit,
  onCancel,
}: {
  survey: TSurvey;
  revealable: Revealable[];
  initial: FormValues;
  submitLabel: string;
  filterNote: string;
  onSubmit: (values: {
    name: string;
    expiresAt: string | null;
    revealedElementIds: string[];
  }) => Promise<void>;
  onCancel?: () => void;
}) => {
  const { t, i18n } = useTranslation();
  const id = useId();
  const [values, setValues] = useState<FormValues>(initial);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const kept = values.keptExpiresAt;
  const expiresAt = kept ? new Date(kept) : apiKeyExpiry(values.expiry, values.customDay);
  const firstDay = firstCustomExpiryDay();
  const formatDate = new Intl.DateTimeFormat(i18n.resolvedLanguage ?? i18n.language, {
    dateStyle: "long",
    timeStyle: "short",
  });

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    // A changed expiry is worked out again from the time of the submit; a kept one is sent as the server gave it.
    const at = kept ? kept : apiKeyExpiry(values.expiry, values.customDay);
    if (at === undefined || isSubmitting) return;
    setIsSubmitting(true);
    try {
      await onSubmit({
        name: values.name.trim(),
        expiresAt: typeof at === "string" ? at : at ? at.toISOString() : null,
        revealedElementIds: values.revealed,
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  const toggleRevealed = (revealableId: string, checked: boolean) =>
    setValues((current) => ({
      ...current,
      revealed: checked
        ? [...current.revealed, revealableId]
        : current.revealed.filter((existing) => existing !== revealableId),
    }));

  return (
    <form onSubmit={(event) => void submit(event)} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor={`${id}-name`}>{t("workspace.analysis.result_shares.name_label")}</Label>
        <Input
          id={`${id}-name`}
          value={values.name}
          maxLength={MAX_NAME_LENGTH}
          placeholder={t("workspace.analysis.result_shares.name_placeholder")}
          onChange={(event) => setValues({ ...values, name: event.target.value })}
        />
      </div>
      <p className="flex items-start gap-2 text-sm text-slate-600">
        <FilterIcon className="mt-0.5 size-4 shrink-0 text-slate-500" aria-hidden="true" />
        {filterNote}
      </p>
      <div className="space-y-2">
        <Label htmlFor={`${id}-expiry`}>{t("workspace.api_keys.expiration")}</Label>
        <Select
          id={`${id}-expiry`}
          value={values.expiry}
          onChange={(event) =>
            setValues({ ...values, expiry: event.target.value as ApiKeyExpiryChoice, keptExpiresAt: null })
          }>
          {API_KEY_EXPIRY_CHOICES.map((choice) => (
            <option key={choice} value={choice}>
              {expiryChoiceLabel(t, choice)}
            </option>
          ))}
        </Select>
        {values.expiry === "custom" && (
          <Input
            type="date"
            aria-label={t("workspace.api_keys.expiration_date")}
            // A kept expiry may be today or past; the browser would refuse to submit a day before min.
            min={kept && values.customDay < firstDay ? values.customDay : firstDay}
            max={LAST_CUSTOM_EXPIRY_DAY}
            value={values.customDay}
            onChange={(event) => setValues({ ...values, customDay: event.target.value, keptExpiresAt: null })}
          />
        )}
        {expiresAt !== undefined && (
          <p className="text-sm text-slate-500">
            {!expiresAt
              ? t("workspace.analysis.result_shares.expiration_never_description")
              : kept && isResultShareExpired({ expiresAt: kept })
                ? t("workspace.analysis.result_shares.expired_on", { date: formatDate.format(expiresAt) })
                : t("workspace.analysis.result_shares.expiration_description", {
                    date: formatDate.format(expiresAt),
                  })}
          </p>
        )}
      </div>
      {revealable.length > 0 && (
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium text-slate-800">
            {t("workspace.analysis.result_shares.reveal_legend")}
          </legend>
          <p className="flex items-start gap-2 text-sm text-slate-600">
            <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-amber-500" aria-hidden="true" />
            {t("workspace.analysis.result_shares.reveal_description")}
          </p>
          <ul className="max-h-48 space-y-2 overflow-y-auto rounded-lg border border-slate-200 p-3">
            {revealable.map((item) => (
              <li key={item.id} className="flex items-center gap-2">
                <Checkbox
                  id={`${id}-reveal-${item.id}`}
                  checked={values.revealed.includes(item.id)}
                  onCheckedChange={(checked) => toggleRevealed(item.id, checked === true)}
                />
                <label htmlFor={`${id}-reveal-${item.id}`} className="text-sm text-slate-700">
                  {revealableLabel(t, survey, item)}
                </label>
              </li>
            ))}
          </ul>
        </fieldset>
      )}
      <div className="flex justify-end gap-2">
        {onCancel && (
          <Button type="button" variant="secondary" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        )}
        <Button type="submit" loading={isSubmitting} disabled={expiresAt === undefined}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
};

/** One link in the list: its address to copy, what it shows, when it stops working, and the ways to change or revoke it. */
const ResultShareRow = ({
  share,
  onEdit,
  onRevoke,
}: {
  share: ResultShare;
  onEdit: () => void;
  onRevoke: () => void;
}) => {
  const { t, i18n } = useTranslation();
  const url = resultShareUrl(window.location.origin, share.key);
  const expired = isResultShareExpired(share);
  const formatDate = new Intl.DateTimeFormat(i18n.resolvedLanguage ?? i18n.language, { dateStyle: "medium" });
  const name = share.name || t("workspace.analysis.result_shares.untitled_link");
  const segmentMissing = share.missingSegmentIds.length > 0;
  return (
    <li className="space-y-2 rounded-lg border border-slate-200 p-3" data-testid="result-share">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <p className="truncate font-medium text-slate-900">{name}</p>
          {expired && <Badge type="error" size="tiny" text={t("common.expired")} />}
          {segmentMissing && (
            <Badge type="warning" size="tiny" text={t("workspace.analysis.result_shares.segment_missing")} />
          )}
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("workspace.analysis.result_shares.edit_link", { name })}
            title={t("workspace.analysis.result_shares.edit_link", { name })}
            onClick={onEdit}>
            <PencilIcon />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("workspace.analysis.result_shares.revoke_link", { name })}
            title={t("workspace.analysis.result_shares.revoke_link", { name })}
            onClick={onRevoke}>
            <TrashIcon />
          </Button>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Input
          readOnly
          value={url}
          aria-label={t("workspace.analysis.result_shares.link_address", { name })}
          className="font-mono text-xs"
          onFocus={(event) => event.target.select()}
        />
        <Button
          variant="secondary"
          size="sm"
          disabled={expired}
          onClick={() => void copyText(url, t, t("workspace.analysis.result_shares.link_copied"))}>
          <CopyIcon />
          {t("common.copy_link")}
        </Button>
      </div>
      <p className="text-xs text-slate-500">
        {[
          hasCriteria(share.filterCriteria)
            ? t("workspace.analysis.result_shares.filtered_responses")
            : t("workspace.analysis.result_shares.all_responses"),
          share.revealedElementIds.length > 0
            ? t("workspace.analysis.result_shares.revealed_count", { count: share.revealedElementIds.length })
            : t("workspace.analysis.result_shares.counts_only"),
          !share.expiresAt
            ? t("workspace.analysis.result_shares.no_expiration")
            : expired
              ? t("workspace.analysis.result_shares.expired_on", {
                  date: formatDate.format(new Date(share.expiresAt)),
                })
              : t("workspace.analysis.result_shares.expires_on", {
                  date: formatDate.format(new Date(share.expiresAt)),
                }),
          share.creator?.name
            ? t("workspace.analysis.result_shares.created_by", { name: share.creator.name })
            : undefined,
        ]
          .filter(Boolean)
          .join(" · ")}
      </p>
      {segmentMissing && (
        <p className="flex items-start gap-2 text-xs text-amber-700">
          <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          {t("workspace.analysis.result_shares.segment_missing_description")}
        </p>
      )}
    </li>
  );
};

/**
 * "Share results": the survey's read-only results links, which show its summary to people without an account. A new link freezes the summary's current filter; cards that list individual answers show only their counts unless the form reveals them. Each link can be copied, changed (name, expiry, revealed cards) or revoked, which stops it at once.
 */
export const ShareResultsDialog = ({
  survey,
  open,
  setOpen,
}: {
  survey: TSurvey;
  open: boolean;
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { filterCriteria } = useResponseFilter();
  const shares = useQuery({ ...resultSharesQuery(survey.id), enabled: open });
  const [editing, setEditing] = useState<ResultShare | null>(null);
  const [revoking, setRevoking] = useState<ResultShare | null>(null);
  const [isRevoking, setIsRevoking] = useState(false);
  // Each opening of the form starts empty; the key remounts it after a link is created.
  const [formKey, setFormKey] = useState(0);
  const filtered = useMemo(() => hasCriteria(filterCriteria), [filterCriteria]);
  const revealable = shares.data?.revealable ?? [];

  const create = async (values: { name: string; expiresAt: string | null; revealedElementIds: string[] }) => {
    try {
      await resultSharesApi.create(survey.id, { ...values, filterCriteria });
      await invalidateResultShares(queryClient, survey.id);
      setFormKey((key) => key + 1);
      // The new link heads the list below, with its Copy button: the clipboard is not written here, since a browser may refuse a write this long after the click.
      toast.success(t("workspace.analysis.result_shares.created"));
    } catch (error) {
      toast.error(
        error instanceof ApiError && error.code === RESULT_SHARE_SEGMENT_NOT_FOUND
          ? t("workspace.analysis.result_shares.segment_not_found")
          : errorMessage(t, error)
      );
    }
  };

  const update = async (
    share: ResultShare,
    values: { name: string; expiresAt: string | null; revealedElementIds: string[] }
  ) => {
    try {
      await resultSharesApi.update(survey.id, share.id, values);
      await invalidateResultShares(queryClient, survey.id);
      setEditing(null);
      toast.success(t("workspace.analysis.result_shares.updated"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const revoke = async () => {
    if (!revoking) return;
    setIsRevoking(true);
    try {
      await resultSharesApi.revoke(survey.id, revoking.id);
      await invalidateResultShares(queryClient, survey.id);
      if (editing?.id === revoking.id) setEditing(null);
      toast.success(t("workspace.analysis.result_shares.revoked"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsRevoking(false);
      setRevoking(null);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={setOpen} width="default">
        <DialogHeader>
          <DialogTitle>{t("workspace.analysis.result_shares.title")}</DialogTitle>
          <DialogDescription>{t("workspace.analysis.result_shares.description")}</DialogDescription>
        </DialogHeader>
        <DialogBody className="space-y-6">
          {shares.isError && (
            <p role="alert" className="text-sm text-red-600">
              {errorMessage(t, shares.error)}
            </p>
          )}
          {shares.isPending ? (
            <div aria-busy="true" className="h-32 animate-pulse rounded-lg bg-slate-100" />
          ) : (
            <>
              <section className="space-y-3">
                <h3 className="text-sm font-semibold text-slate-900">
                  {editing
                    ? t("workspace.analysis.result_shares.edit_title", {
                        name: editing.name || t("workspace.analysis.result_shares.untitled_link"),
                      })
                    : t("workspace.analysis.result_shares.create_title")}
                </h3>
                {editing ? (
                  <ResultShareForm
                    key={`edit-${editing.id}`}
                    survey={survey}
                    revealable={revealable}
                    initial={formValues(editing, revealable)}
                    submitLabel={t("common.save")}
                    filterNote={
                      hasCriteria(editing.filterCriteria)
                        ? t("workspace.analysis.result_shares.filter_frozen_filtered")
                        : t("workspace.analysis.result_shares.filter_frozen_all")
                    }
                    onSubmit={(values) => update(editing, values)}
                    onCancel={() => setEditing(null)}
                  />
                ) : (
                  <ResultShareForm
                    key={`new-${formKey}`}
                    survey={survey}
                    revealable={revealable}
                    initial={NEW_LINK}
                    submitLabel={t("workspace.analysis.result_shares.create")}
                    filterNote={
                      filtered
                        ? t("workspace.analysis.result_shares.filter_note_filtered")
                        : t("workspace.analysis.result_shares.filter_note_all")
                    }
                    onSubmit={create}
                  />
                )}
              </section>
              <section className="space-y-3">
                <h3 className="text-sm font-semibold text-slate-900">
                  {t("workspace.analysis.result_shares.links_title")}
                </h3>
                {shares.data && shares.data.shares.length > 0 ? (
                  <ul className="space-y-3">
                    {shares.data.shares.map((share) => (
                      <ResultShareRow
                        key={share.id}
                        share={share}
                        onEdit={() => setEditing(share)}
                        onRevoke={() => setRevoking(share)}
                      />
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-slate-500">{t("workspace.analysis.result_shares.no_links")}</p>
                )}
              </section>
            </>
          )}
        </DialogBody>
      </Dialog>
      <ConfirmationModal
        open={revoking !== null}
        setOpen={(next) => {
          if (!next) setRevoking(null);
        }}
        title={t("workspace.analysis.result_shares.revoke_title")}
        body={t("workspace.analysis.result_shares.revoke_body")}
        buttonText={t("workspace.analysis.result_shares.revoke")}
        onConfirm={() => void revoke()}
        buttonVariant="destructive"
        buttonLoading={isRevoking}
      />
    </>
  );
};
