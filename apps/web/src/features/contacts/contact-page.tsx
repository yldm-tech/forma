import { useQuery, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ArrowLeftIcon,
  CalendarIcon,
  HashIcon,
  LinkIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  TagIcon,
  TrashIcon,
} from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { FormError } from "../../components/ui/form-error";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { PageContentWrapper, PageHeader } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../components/ui/tooltip";
import { type AttributeKey, PROTECTED_ATTRIBUTE_KEYS, attributeKeyLabel } from "../../lib/attribute-keys";
import { cn } from "../../lib/cn";
import {
  type AttributeRow,
  attributeRowsErrors,
  attributeUpdateMessage,
  attributesPayload,
  dayToStoredValue,
  storedDayForInput,
} from "../../lib/contact-attributes-form";
import { contactIdentifier, formatAttributeValue } from "../../lib/contacts";
import { errorMessage } from "../../lib/errors";
import { meQuery } from "../../lib/queries/auth";
import {
  type ContactAttribute,
  attributeKeysQuery,
  contactActivityQuery,
  contactKeys,
  contactQuery,
  publishedLinkSurveysQuery,
  useDeleteContacts,
  useUpdateContactAttributes,
} from "../../lib/queries/contacts";
import { tagsQuery } from "../../lib/queries/tags";
import { toast } from "../../lib/toast";
import { toResponseTag } from "../survey-analysis/api";
import { ActivityTimeline } from "./contact-activity";
import { GeneratePersonalLinkModal } from "./generate-personal-link-modal";

const DATA_TYPE_ICONS = { date: CalendarIcon, number: HashIcon, string: TagIcon } as const;

/**
 * forma2's single contact page: the contact's system and custom attributes with how many responses and displays it has, its activity, and the actions on it.
 */
export const ContactPage = ({
  workspaceId,
  contactId,
  isReadOnly,
}: {
  workspaceId: string;
  contactId: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const { data: detail } = useSuspenseQuery(contactQuery(workspaceId, contactId));
  const { data: activity } = useSuspenseQuery(contactActivityQuery(workspaceId, contactId));
  const { data: me } = useSuspenseQuery(meQuery);
  const keys = useQuery(attributeKeysQuery(workspaceId));
  const tags = useQuery(tagsQuery(workspaceId));
  const title = contactIdentifier(
    Object.fromEntries(detail.attributes.map((attr) => [attr.key, attr.value]))
  );
  return (
    <PageContentWrapper>
      <Link
        to="/workspaces/$workspaceId/contacts"
        params={{ workspaceId }}
        className="inline-flex items-center gap-2 text-sm text-slate-600 hover:text-slate-900">
        <ArrowLeftIcon className="size-4" />
        {t("common.back")}
      </Link>
      <PageHeader
        pageTitle={title}
        cta={
          <ContactControlBar
            workspaceId={workspaceId}
            contactId={contactId}
            isReadOnly={isReadOnly}
            currentAttributes={detail.attributes}
            attributeKeys={keys.data ?? []}
          />
        }
      />
      <section className="pt-6 pb-24">
        <div className="grid grid-cols-1 gap-y-10 sm:grid-cols-4 sm:gap-x-8">
          <AttributesSection
            contactId={contactId}
            attributes={detail.attributes}
            responseCount={activity.responseCount}
            displayCount={activity.displayCount}
          />
          <ActivityTimeline
            workspaceId={workspaceId}
            contactId={contactId}
            activity={activity}
            environmentTags={(tags.data ?? []).map(toResponseTag)}
            locale={me.locale}
            isReadOnly={isReadOnly}
          />
        </div>
      </section>
    </PageContentWrapper>
  );
};

const AttributesSection = ({
  contactId,
  attributes,
  responseCount,
  displayCount,
}: {
  contactId: string;
  attributes: ContactAttribute[];
  responseCount: number;
  displayCount: number;
}) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const byLabel = (a: ContactAttribute, b: ContactAttribute) =>
    (a.name || a.key).localeCompare(b.name || b.key);
  const system = attributes.filter((attr) => attr.type === "default").sort(byLabel);
  const custom = attributes.filter((attr) => attr.type === "custom").sort(byLabel);

  const value = (attr: ContactAttribute) => {
    if (!attr.value) return <span className="text-slate-300">{t("workspace.contacts.not_provided")}</span>;
    if (attr.key === "userId") return <IdBadge id={attr.value} />;
    return formatAttributeValue(attr.value, attr.dataType, me.locale);
  };
  const row = (attr: ContactAttribute) => {
    const Icon = DATA_TYPE_ICONS[attr.dataType];
    return (
      <div key={attr.key}>
        <dt className="flex items-center gap-2 text-sm font-medium text-slate-500">
          <Icon className="size-4 text-slate-400" />
          <span>{attr.name || attr.key}</span>
        </dt>
        <dd className="mt-1 text-sm text-slate-900">{value(attr)}</dd>
      </div>
    );
  };

  return (
    <dl className="space-y-6">
      <h2 className="text-lg font-bold text-slate-700">{t("workspace.contacts.system_attributes")}</h2>
      {system.map(row)}
      <div>
        <dt className="flex items-center gap-2 text-sm font-medium text-slate-500">
          <TagIcon className="size-4 text-slate-400" />
          <span>{t("workspace.contacts.contact_id")}</span>
        </dt>
        {/* A UUID does not fit the column on one line. Both properties are inherited, so the badge's own markup needs no targeting: it may break anywhere, and the two lines come out about equally long instead of leaving the last characters alone on a line. */}
        <dd className="mt-1 text-sm text-balance break-all text-slate-900">
          <IdBadge id={contactId} />
        </dd>
      </div>
      {custom.length > 0 && (
        <>
          <hr />
          <h2 className="text-lg font-bold text-slate-700">{t("workspace.contacts.custom_attributes")}</h2>
          {custom.map(row)}
        </>
      )}
      <hr />
      <div>
        <dt className="text-sm font-medium text-slate-500">{t("common.responses")}</dt>
        <dd className="mt-1 text-sm text-slate-900">{responseCount}</dd>
      </div>
      <div>
        <dt className="text-sm font-medium text-slate-500">{t("workspace.contacts.displays")}</dt>
        <dd className="mt-1 text-sm text-slate-900">{displayCount}</dd>
      </div>
    </dl>
  );
};

/** forma2's ContactControlBar: refresh, and for writers edit attributes, generate a personal link and delete. */
const ContactControlBar = ({
  workspaceId,
  contactId,
  isReadOnly,
  currentAttributes,
  attributeKeys,
}: {
  workspaceId: string;
  contactId: string;
  isReadOnly: boolean;
  currentAttributes: ContactAttribute[];
  attributeKeys: AttributeKey[];
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const remove = useDeleteContacts(workspaceId);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [linkOpen, setLinkOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const publishedLinkSurveys = useQuery({ ...publishedLinkSurveysQuery(workspaceId), enabled: !isReadOnly });

  const refresh = async () => {
    setRefreshing(true);
    await queryClient.invalidateQueries({ queryKey: contactKeys.detail(workspaceId, contactId) });
    setRefreshing(false);
  };

  const deleteContact = async () => {
    try {
      await remove.mutateAsync([contactId]);
      toast.success(t("workspace.contacts.contact_deleted_successfully"));
      await navigate({ href: `/workspaces/${workspaceId}/contacts` });
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setConfirmDelete(false);
    }
  };

  // Each icon button shows its name as a tooltip on hover and focus, so sighted users need not guess what it does.
  const iconButton = (label: string, icon: ReactNode, onClick: () => void, disabled = false) => (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={label} disabled={disabled} onClick={onClick}>
          {icon}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );

  return (
    <>
      <TooltipProvider delayDuration={300}>
        <div className="flex items-center gap-1 rounded-md border border-slate-200 bg-white p-1">
          {iconButton(
            t("common.refresh"),
            <RefreshCwIcon className={cn("size-4", refreshing && "animate-spin")} />,
            () => void refresh(),
            refreshing
          )}
          {!isReadOnly && (
            <>
              {iconButton(t("workspace.contacts.edit_attributes"), <PencilIcon className="size-4" />, () =>
                setEditOpen(true)
              )}
              {iconButton(
                t("workspace.contacts.generate_personal_link"),
                <LinkIcon className="size-4" />,
                () => setLinkOpen(true)
              )}
              {iconButton(t("common.delete"), <TrashIcon className="size-4" />, () => setConfirmDelete(true))}
            </>
          )}
        </div>
      </TooltipProvider>
      <DeleteDialog
        open={confirmDelete}
        setOpen={setConfirmDelete}
        deleteWhat={t("common.contact")}
        onDelete={() => void deleteContact()}
        isDeleting={remove.isPending}
        text={t("workspace.contacts.delete_contact_confirmation_with_quotas", { value: 1 })}
      />
      <GeneratePersonalLinkModal
        open={linkOpen}
        setOpen={setLinkOpen}
        contactId={contactId}
        publishedLinkSurveys={publishedLinkSurveys.data ?? []}
      />
      {editOpen && (
        <EditContactAttributesModal
          workspaceId={workspaceId}
          contactId={contactId}
          currentAttributes={currentAttributes}
          attributeKeys={attributeKeys}
          setOpen={setEditOpen}
        />
      )}
    </>
  );
};

/** forma2's EditContactAttributesModal: the contact's values, one row per key; saved keys keep their key, the default keys cannot be removed, and a key left out is deleted. */
const EditContactAttributesModal = ({
  workspaceId,
  contactId,
  currentAttributes,
  attributeKeys,
  setOpen,
}: {
  workspaceId: string;
  contactId: string;
  currentAttributes: ContactAttribute[];
  attributeKeys: AttributeKey[];
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const update = useUpdateContactAttributes(workspaceId, contactId);
  const [rows, setRows] = useState<AttributeRow[]>(() =>
    currentAttributes.map((attr) => ({ key: attr.key, value: attr.value ?? "" }))
  );
  const [submitted, setSubmitted] = useState(false);
  const savedKeys = new Set(currentAttributes.map((attr) => attr.key));
  const validation = attributeRowsErrors(rows, attributeKeys, t);
  const errors = submitted ? validation : { rows: {} as typeof validation.rows };
  const setRow = (index: number, patch: Partial<AttributeRow>) =>
    setRows((current) => current.map((row, i) => (i === index ? { ...row, ...patch } : row)));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (validation.form || Object.keys(validation.rows).length > 0) return;
    try {
      const result = await update.mutateAsync(attributesPayload(rows, attributeKeys));
      toast.success(t("workspace.contacts.edit_attributes_success"));
      for (const message of [...(result.messages ?? []), ...(result.errors ?? [])]) {
        toast.error(attributeUpdateMessage(message.code, message.params, t));
      }
      setOpen(false);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open onOpenChange={setOpen}>
      <DialogHeader>
        <DialogTitle>{t("workspace.contacts.edit_attribute_values")}</DialogTitle>
        <DialogDescription>{t("workspace.contacts.edit_attribute_values_description")}</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} className="flex min-h-0 flex-col gap-4" noValidate>
        <DialogBody>
          <div className="space-y-4">
            {rows.map((row, index) => {
              const taken = new Set(rows.filter((_, i) => i !== index).map((r) => r.key));
              const options = attributeKeys.filter((key) => !taken.has(key.key));
              const dataType = attributeKeys.find((key) => key.key === row.key)?.dataType ?? "string";
              const rowErrors = errors.rows[index] ?? {};
              return (
                // oxlint-disable-next-line react/no-array-index-key -- rows are positional while their key is being chosen
                <div key={index} className="flex gap-2">
                  <div className="flex-1 space-y-1">
                    <Label htmlFor={`attribute-key-${index}`}>{t("workspace.contacts.attribute_key")}</Label>
                    <Select
                      id={`attribute-key-${index}`}
                      value={row.key}
                      disabled={savedKeys.has(row.key)}
                      onChange={(e) => setRow(index, { key: e.target.value })}>
                      <option value="" disabled>
                        {t("workspace.contacts.select_attribute_key")}
                      </option>
                      {options.map((key) => (
                        <option key={key.id} value={key.key}>
                          {attributeKeyLabel(key)}
                        </option>
                      ))}
                    </Select>
                    <FormError>{rowErrors.key}</FormError>
                  </div>
                  <div className="flex-1 space-y-1">
                    <Label htmlFor={`attribute-value-${index}`}>
                      {t("workspace.contacts.attribute_value")}
                    </Label>
                    <div className="flex gap-x-2">
                      {dataType === "date" ? (
                        <Input
                          id={`attribute-value-${index}`}
                          type="date"
                          value={storedDayForInput(row.value)}
                          isInvalid={!!rowErrors.value}
                          onChange={(e) => setRow(index, { value: dayToStoredValue(e.target.value) })}
                        />
                      ) : (
                        <Input
                          id={`attribute-value-${index}`}
                          type={dataType === "number" ? "number" : "text"}
                          value={row.value}
                          isInvalid={!!rowErrors.value}
                          placeholder={t("workspace.contacts.attribute_value_placeholder")}
                          onChange={(e) => setRow(index, { value: e.target.value })}
                        />
                      )}
                      <Button
                        type="button"
                        variant="secondary"
                        size="icon"
                        aria-label={t("common.remove")}
                        disabled={PROTECTED_ATTRIBUTE_KEYS.includes(row.key)}
                        onClick={() => setRows((current) => current.filter((_, i) => i !== index))}>
                        <TrashIcon className="size-4" />
                      </Button>
                    </div>
                    <FormError>{rowErrors.value}</FormError>
                  </div>
                </div>
              );
            })}
            {rows.length < attributeKeys.length && (
              <Button
                type="button"
                variant="secondary"
                className="w-fit"
                onClick={() => setRows([...rows, { key: "", value: "" }])}>
                <PlusIcon className="mr-2 size-4" />
                {t("workspace.contacts.add_attribute")}
              </Button>
            )}
            <FormError>{errors.form}</FormError>
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={update.isPending}>
            {t("common.save_changes")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
