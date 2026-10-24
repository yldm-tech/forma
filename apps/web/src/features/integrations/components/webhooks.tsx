import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { CheckIcon, CopyIcon, TrashIcon, WebhookIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Checkbox } from "../../../components/ui/checkbox";
import { DeleteDialog } from "../../../components/ui/delete-dialog";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import { EmptyState } from "../../../components/ui/empty-state";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { PageContentWrapper, PageHeader } from "../../../components/ui/page";
import { cn } from "../../../lib/cn";
import { formatDateTimeForDisplay, timeSince } from "../../../lib/time";
import { toast } from "../../../lib/toast";
import {
  type Webhook,
  type WebhookTrigger,
  testWebhookEndpoint,
  useCreateWebhook,
  useDeleteWebhook,
  useUpdateWebhook,
  webhookSurveysQuery,
  webhooksQuery,
} from "../api";
import { toggled } from "../links";
import {
  WEBHOOK_TRIGGERS,
  endpointTestFailure,
  isDiscordWebhook,
  triggerLabel,
  triggersText,
  validWebHookURL,
  webhookErrorMessage,
  webhookSurveyNames,
} from "../webhooks";
import { BackToIntegrations } from "./shared";

type SurveyOption = { id: string; name: string };

/** forma2's webhooks page: the workspace's webhooks, newest first, and adding one when the caller may write to the workspace. */
export const WebhooksPage = ({ workspaceId, isReadOnly }: { workspaceId: string; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const { data } = useSuspenseQuery(webhooksQuery(workspaceId));
  const surveys = useQuery(webhookSurveysQuery(workspaceId)).data ?? [];
  const [isAddOpen, setIsAddOpen] = useState(false);
  const [active, setActive] = useState<Webhook | null>(null);

  return (
    <PageContentWrapper>
      <BackToIntegrations workspaceId={workspaceId} />
      <PageHeader
        pageTitle={t("common.webhooks")}
        cta={
          !isReadOnly && (
            <Button size="sm" onClick={() => setIsAddOpen(true)}>
              <WebhookIcon className="mr-2 size-5 text-white" />
              {t("workspace.integrations.webhooks.add_webhook")}
            </Button>
          )
        }
      />
      {data.webhooks.length === 0 ? (
        <EmptyState text={t("workspace.integrations.webhooks.empty_webhook_message")} />
      ) : (
        <div className="rounded-lg border border-slate-200">
          <WebhookTableHeading />
          <div className="grid-cols-7">
            {data.webhooks.map((webhook) => (
              <button type="button" key={webhook.id} className="w-full" onClick={() => setActive(webhook)}>
                <WebhookRow webhook={webhook} surveys={surveys} />
              </button>
            ))}
          </div>
        </div>
      )}
      {isAddOpen && (
        <AddWebhookModal
          workspaceId={workspaceId}
          surveys={surveys}
          allowInternalUrls={data.allowInternalUrls}
          onClose={() => setIsAddOpen(false)}
        />
      )}
      {active && (
        <WebhookDetailModal
          webhook={active}
          surveys={surveys}
          isReadOnly={isReadOnly}
          allowInternalUrls={data.allowInternalUrls}
          onClose={() => setActive(null)}
        />
      )}
    </PageContentWrapper>
  );
};

const WebhookTableHeading = () => {
  const { t } = useTranslation();
  return (
    <div className="grid h-12 grid-cols-12 content-center rounded-lg bg-slate-100 text-left text-sm font-semibold text-slate-900">
      <span className="sr-only">{t("common.edit")}</span>
      <div className="col-span-3 pl-6">{t("common.webhook")}</div>
      <div className="col-span-1 text-center">{t("workspace.integrations.webhooks.source")}</div>
      <div className="col-span-4 text-center">{t("common.surveys")}</div>
      <div className="col-span-2 text-center">{t("workspace.integrations.webhooks.triggers")}</div>
      <div className="col-span-2 text-center">{t("common.updated")}</div>
    </div>
  );
};

const WebhookRow = ({ webhook, surveys }: { webhook: Webhook; surveys: readonly SurveyOption[] }) => {
  const { t, i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language ?? "en-US";
  const surveyNames = webhookSurveyNames(webhook, surveys);
  return (
    <div className="mt-2 grid h-auto grid-cols-12 content-center rounded-lg py-2 hover:bg-slate-100">
      <div className="col-span-3 flex items-center truncate pl-6 text-left text-sm">
        {webhook.name ? (
          <div>
            <div className="font-medium text-slate-900">{webhook.name}</div>
            <div className="text-xs text-slate-400">{webhook.url}</div>
          </div>
        ) : (
          <div className="font-medium text-slate-900">{webhook.url}</div>
        )}
      </div>
      <div className="col-span-1 my-auto text-center text-sm text-slate-800">
        <Badge type="gray" size="tiny" text={webhook.source || t("common.user")} className="capitalize" />
      </div>
      <div className="col-span-4 my-auto text-center text-sm text-slate-800">
        {surveyNames.length === 0 ? (
          <p className="text-slate-400">-</p>
        ) : (
          <p className="truncate text-slate-400" title={surveyNames.join(", ")}>
            {surveyNames.join(", ")}
          </p>
        )}
      </div>
      <div className="col-span-2 my-auto text-center text-sm text-slate-800">
        <p className="text-slate-400">
          {webhook.triggers.length === 0
            ? t("workspace.integrations.webhooks.no_triggers")
            : triggersText(t, webhook.triggers)}
        </p>
      </div>
      <div className="col-span-2 my-auto text-center text-sm whitespace-nowrap text-slate-500">
        {timeSince(webhook.updatedAt, locale)}
      </div>
    </div>
  );
};

const TriggerCheckboxGroup = ({
  selected,
  onToggle,
  allowChanges,
}: {
  selected: readonly WebhookTrigger[];
  onToggle: (trigger: WebhookTrigger) => void;
  allowChanges: boolean;
}) => {
  const { t } = useTranslation();
  return (
    <div className="mt-1 rounded-lg border border-slate-200">
      <div className="grid content-center rounded-lg bg-slate-50 p-3 text-left text-sm text-slate-900">
        {WEBHOOK_TRIGGERS.map((trigger) => (
          <div key={trigger} className="my-1 flex items-center gap-x-2">
            <label
              htmlFor={trigger}
              className={cn(
                "flex items-center",
                allowChanges ? "cursor-pointer" : "cursor-not-allowed opacity-50"
              )}>
              <Checkbox
                id={trigger}
                className="bg-white"
                checked={selected.includes(trigger)}
                disabled={!allowChanges}
                onCheckedChange={() => allowChanges && onToggle(trigger)}
              />
              <span className="ml-2">{triggerLabel(t, trigger)}</span>
            </label>
          </div>
        ))}
      </div>
    </div>
  );
};

const SurveyCheckboxGroup = ({
  surveys,
  selected,
  allSurveys,
  onToggleAll,
  onToggle,
  allowChanges,
}: {
  surveys: readonly SurveyOption[];
  selected: readonly string[];
  allSurveys: boolean;
  onToggleAll: () => void;
  onToggle: (surveyId: string) => void;
  allowChanges: boolean;
}) => {
  const { t } = useTranslation();
  return (
    <div className="mt-1 max-h-[15vh] overflow-y-auto rounded-lg border border-slate-200">
      <div className="grid content-center rounded-lg bg-slate-50 p-3 text-left text-sm text-slate-900">
        <div className="my-1 flex items-center gap-x-2">
          <label
            htmlFor="allSurveys"
            className={cn(
              "flex items-center",
              allSurveys && "font-semibold",
              allowChanges ? "cursor-pointer" : "cursor-not-allowed opacity-50"
            )}>
            <Checkbox
              id="allSurveys"
              className="bg-white"
              checked={allSurveys}
              onCheckedChange={onToggleAll}
              disabled={!allowChanges}
            />
            <span className="ml-2">{t("workspace.integrations.webhooks.all_current_and_new_surveys")}</span>
          </label>
        </div>
        {surveys.map((survey) => (
          <div key={survey.id} className="my-1 flex items-center gap-x-2">
            <label
              htmlFor={`survey-${survey.id}`}
              className={cn(
                "flex items-center",
                allSurveys || !allowChanges ? "cursor-not-allowed opacity-50" : "cursor-pointer"
              )}>
              <Checkbox
                id={`survey-${survey.id}`}
                className="bg-white"
                checked={selected.includes(survey.id) && !allSurveys}
                disabled={allSurveys || !allowChanges}
                onCheckedChange={() => allowChanges && onToggle(survey.id)}
              />
              <span className="ml-2">{survey.name}</span>
            </label>
          </div>
        ))}
      </div>
    </div>
  );
};

/** The URL field's border after an endpoint test: green when it answered, red when it did not. */
const endpointClass = (accessible: boolean | undefined) =>
  accessible === true
    ? "border-green-500 bg-green-50"
    : accessible === false
      ? "border-red-200 bg-red-50"
      : "border-slate-200 bg-white";

/** The endpoint test both forms run before saving, forma2's handleTestEndpoint: the form's URL check, then one signed test request from the server. */
const useEndpointTest = (workspaceId: string, allowInternalUrls: boolean) => {
  const { t } = useTranslation();
  const [hitting, setHitting] = useState(false);
  const [accessible, setAccessible] = useState<boolean>();
  const run = async (
    input: { url: string; webhookId?: string; secret?: string },
    sendSuccessToast: boolean
  ): Promise<{ success: boolean; secret?: string }> => {
    const check = validWebHookURL(input.url, allowInternalUrls);
    if (!check.valid) {
      toast.error(check.error);
      return { success: false };
    }
    setHitting(true);
    try {
      const result = await testWebhookEndpoint(workspaceId, input);
      if (sendSuccessToast) toast.success(t("workspace.integrations.webhooks.endpoint_pinged"));
      setAccessible(true);
      return result;
    } catch (error) {
      const message = webhookErrorMessage(t, error);
      toast.error(endpointTestFailure(t, message));
      console.error(t("workspace.integrations.webhooks.webhook_test_failed_due_to"), message);
      setAccessible(false);
      return { success: false };
    } finally {
      setHitting(false);
    }
  };
  return { run, hitting, accessible };
};

/** forma2's AddWebhookModal: the endpoint is tested before the webhook is created, and the secret that signed the test becomes the webhook's, shown once afterwards. */
export const AddWebhookModal = ({
  workspaceId,
  surveys,
  allowInternalUrls,
  onClose,
}: {
  workspaceId: string;
  surveys: readonly SurveyOption[];
  allowInternalUrls: boolean;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const create = useCreateWebhook(workspaceId);
  const endpoint = useEndpointTest(workspaceId, allowInternalUrls);
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [triggers, setTriggers] = useState<WebhookTrigger[]>([]);
  const [surveyIds, setSurveyIds] = useState<string[]>([]);
  const [allSurveys, setAllSurveys] = useState(false);
  const [secret, setSecret] = useState<string>();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Webhook | null>(null);

  const test = async (sendSuccessToast: boolean) => {
    const result = await endpoint.run({ url, secret }, sendSuccessToast);
    if (result.secret) setSecret(result.secret);
    return result;
  };

  const submit = async () => {
    if (url === "") {
      toast.error(t("workspace.integrations.webhooks.please_enter_a_url"));
      return;
    }
    if (triggers.length === 0) {
      toast.error(t("common.please_select_at_least_one_trigger"));
      return;
    }
    if (!allSurveys && surveyIds.length === 0) {
      toast.error(t("common.please_select_at_least_one_survey"));
      return;
    }
    if (isDiscordWebhook(url)) {
      toast.error(t("workspace.integrations.webhooks.discord_webhook_not_supported"));
      return;
    }
    setCreating(true);
    try {
      const tested = await test(false);
      if (!tested.success) return;
      const webhook = await create.mutateAsync({
        name,
        url,
        source: "user",
        triggers,
        surveyIds: allSurveys ? [] : surveyIds,
        allSurveys,
        webhookSecret: tested.secret,
      });
      setCreated(webhook);
      toast.success(t("workspace.integrations.webhooks.webhook_added_successfully"));
    } catch (error) {
      toast.error(webhookErrorMessage(t, error));
    } finally {
      setCreating(false);
    }
  };

  if (created) return <WebhookCreatedModal webhook={created} onClose={onClose} />;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <WebhookIcon />
        <DialogTitle>{t("workspace.integrations.webhooks.add_webhook")}</DialogTitle>
        <DialogDescription>{t("workspace.integrations.webhooks.add_webhook_description")}</DialogDescription>
      </DialogHeader>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}>
        <DialogBody className="space-y-4 pb-4">
          <div>
            <Label htmlFor="webhook-name">{t("common.name")}</Label>
            <Input
              id="webhook-name"
              className="mt-1"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("workspace.integrations.webhooks.webhook_name_placeholder")}
            />
          </div>
          <div>
            <Label htmlFor="webhook-url">{t("common.url")}</Label>
            <div className="mt-1 flex">
              <Input
                id="webhook-url"
                type="url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                className={endpointClass(endpoint.accessible)}
                placeholder={t("workspace.integrations.webhooks.webhook_url_placeholder")}
              />
              <Button
                type="button"
                variant="secondary"
                loading={endpoint.hitting}
                className="ml-2 whitespace-nowrap"
                disabled={url.trim() === ""}
                onClick={() => void test(true)}>
                {t("workspace.integrations.webhooks.test_endpoint")}
              </Button>
            </div>
          </div>
          <div>
            <Label>{t("workspace.integrations.webhooks.triggers")}</Label>
            <TriggerCheckboxGroup
              selected={triggers}
              onToggle={(trigger) => setTriggers((current) => toggled(current, trigger))}
              allowChanges
            />
          </div>
          <div>
            <Label>{t("common.surveys")}</Label>
            <SurveyCheckboxGroup
              surveys={surveys}
              selected={surveyIds}
              allSurveys={allSurveys}
              onToggleAll={() => {
                setAllSurveys((all) => !all);
                setSurveyIds([]);
              }}
              onToggle={(id) => setSurveyIds((current) => toggled(current, id))}
              allowChanges
            />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={creating}>
            {t("workspace.integrations.webhooks.add_webhook")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};

/** forma2's WebhookCreatedModal: the signing secret, shown this once. */
export const WebhookCreatedModal = ({ webhook, onClose }: { webhook: Webhook; onClose: () => void }) => {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);

  const copy = async () => {
    await navigator.clipboard.writeText(webhook.secret ?? "");
    setCopied(true);
    toast.success(t("common.copied_to_clipboard"));
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <CheckIcon className="size-6 text-green-500" />
        <DialogTitle>{t("workspace.integrations.webhooks.webhook_created")}</DialogTitle>
        <DialogDescription>{t("workspace.integrations.webhooks.copy_secret_now")}</DialogDescription>
      </DialogHeader>
      <DialogBody className="space-y-4 pb-4">
        <div>
          <Label htmlFor="webhook-secret">{t("workspace.integrations.webhooks.signing_secret")}</Label>
          <div className="mt-1 flex">
            <Input
              id="webhook-secret"
              type="text"
              readOnly
              value={webhook.secret ?? ""}
              className="font-mono text-sm"
            />
            <Button
              type="button"
              variant="secondary"
              className="ml-2 whitespace-nowrap"
              onClick={() => void copy()}>
              {copied ? (
                <>
                  <CheckIcon className="size-4" />
                  {t("common.copied")}
                </>
              ) : (
                <>
                  <CopyIcon className="size-4" />
                  {t("common.copy")}
                </>
              )}
            </Button>
          </div>
          <p className="mt-2 text-xs text-slate-500">
            {t("workspace.integrations.webhooks.secret_copy_warning")}
          </p>
        </div>
      </DialogBody>
      <DialogFooter>
        <Button type="button" onClick={onClose}>
          {t("common.done")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};

/** forma2's WebhookModal: a webhook's overview, and its settings. */
export const WebhookDetailModal = ({
  webhook,
  surveys,
  isReadOnly,
  allowInternalUrls,
  onClose,
}: {
  webhook: Webhook;
  surveys: readonly SurveyOption[];
  isReadOnly: boolean;
  allowInternalUrls: boolean;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"overview" | "settings">("overview");
  const tabs = [
    { id: "overview" as const, title: t("common.overview") },
    { id: "settings" as const, title: t("common.settings") },
  ];
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()} disableCloseOnOutsideClick>
      <DialogHeader>
        <WebhookIcon />
        <DialogTitle>{webhook.name || t("common.webhook")}</DialogTitle>
        <DialogDescription>{webhook.url}</DialogDescription>
      </DialogHeader>
      <DialogBody>
        <div className="flex h-full w-full flex-col">
          <div
            role="tablist"
            className="flex w-full items-center justify-center gap-x-2 border-b border-slate-200 px-6">
            {tabs.map((item) => (
              <button
                key={item.id}
                type="button"
                role="tab"
                aria-selected={tab === item.id}
                className={cn(
                  "mr-4 px-1 pb-3 focus:outline-hidden",
                  tab === item.id
                    ? "border-b-2 border-brand-dark font-semibold text-slate-900"
                    : "text-slate-500 hover:text-slate-700"
                )}
                onClick={() => setTab(item.id)}>
                {item.title}
              </button>
            ))}
          </div>
          <div className="flex-1 overflow-y-auto pt-4">
            {tab === "overview" ? (
              <WebhookOverview webhook={webhook} surveys={surveys} />
            ) : (
              <WebhookSettings
                webhook={webhook}
                surveys={surveys}
                isReadOnly={isReadOnly}
                allowInternalUrls={allowInternalUrls}
                onClose={onClose}
              />
            )}
          </div>
        </div>
      </DialogBody>
    </Dialog>
  );
};

const WebhookOverview = ({ webhook, surveys }: { webhook: Webhook; surveys: readonly SurveyOption[] }) => {
  const { t, i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language ?? "en-US";
  return (
    <div className="grid grid-cols-3 pb-2">
      <div className="col-span-2 space-y-4 pr-6">
        <div>
          <Label className="text-slate-500">{t("common.name")}</Label>
          <p className="truncate text-sm text-slate-900">{webhook.name ? webhook.name : "-"}</p>
        </div>
        <div>
          <Label className="text-slate-500">
            {t("workspace.integrations.webhooks.created_by_third_party")}
          </Label>
          <p className="text-sm text-slate-900 capitalize">
            {webhook.source === "user" ? "No" : webhook.source}
          </p>
        </div>
        <div>
          <Label className="text-slate-500">{t("common.url")}</Label>
          <p className="text-sm text-slate-900">{webhook.url}</p>
        </div>
        <div>
          <Label className="text-slate-500">{t("common.surveys")}</Label>
          {webhookSurveyNames(webhook, surveys).map((name, index) => (
            <p key={index} className="text-sm text-slate-900">
              {name}
            </p>
          ))}
        </div>
        <div>
          <Label className="text-slate-500">{t("workspace.integrations.webhooks.triggers")}</Label>
          {webhook.triggers.map((trigger) => (
            <p key={trigger} className="text-sm text-slate-900">
              {triggerLabel(t, trigger)}
            </p>
          ))}
        </div>
      </div>
      <div className="col-span-1 space-y-3 rounded-lg border border-slate-100 bg-slate-50 p-2">
        <div>
          <Label className="text-xs font-normal text-slate-500">{t("common.created_at")}</Label>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(webhook.createdAt, locale)}</p>
        </div>
        <div>
          <Label className="text-xs font-normal text-slate-500">{t("common.updated_at")}</Label>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(webhook.updatedAt, locale)}</p>
        </div>
      </div>
    </div>
  );
};

/** forma2's WebhookSettingsTab: edit (the endpoint is tested again, signed with the stored secret) or delete. A webhook a third party created keeps its URL, triggers and surveys. */
const WebhookSettings = ({
  webhook,
  surveys,
  isReadOnly,
  allowInternalUrls,
  onClose,
}: {
  webhook: Webhook;
  surveys: readonly SurveyOption[];
  isReadOnly: boolean;
  allowInternalUrls: boolean;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const update = useUpdateWebhook(webhook.workspaceId);
  const remove = useDeleteWebhook(webhook.workspaceId);
  const endpoint = useEndpointTest(webhook.workspaceId, allowInternalUrls);
  const [name, setName] = useState(webhook.name ?? "");
  const [url, setUrl] = useState(webhook.url);
  const [triggers, setTriggers] = useState<WebhookTrigger[]>(webhook.triggers);
  const [surveyIds, setSurveyIds] = useState<string[]>(webhook.surveyIds);
  const [allSurveys, setAllSurveys] = useState(webhook.allSurveys);
  const [isDeleteOpen, setIsDeleteOpen] = useState(false);
  const isThirdParty = webhook.source !== "user";
  const allowChanges = !isThirdParty && !isReadOnly;

  const save = async () => {
    if (triggers.length === 0) {
      toast.error(t("common.please_select_at_least_one_trigger"));
      return;
    }
    if (!allSurveys && surveyIds.length === 0) {
      toast.error(t("common.please_select_at_least_one_survey"));
      return;
    }
    const tested = await endpoint.run({ url, webhookId: webhook.id }, false);
    if (!tested.success) return;
    try {
      await update.mutateAsync({
        webhookId: webhook.id,
        input: {
          name,
          url,
          source: webhook.source,
          triggers,
          surveyIds: allSurveys ? [] : surveyIds,
          allSurveys,
        },
      });
      toast.success(t("workspace.integrations.webhooks.webhook_updated_successfully"));
    } catch (error) {
      toast.error(webhookErrorMessage(t, error));
    }
    onClose();
  };

  const deleteWebhook = async () => {
    setIsDeleteOpen(false);
    onClose();
    try {
      await remove.mutateAsync(webhook.id);
      toast.success(t("workspace.integrations.webhooks.webhook_deleted_successfully"));
    } catch (error) {
      toast.error(webhookErrorMessage(t, error));
    }
  };

  return (
    <div>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}>
        <div>
          <Label htmlFor="webhook-name">{t("common.name")}</Label>
          <Input
            id="webhook-name"
            className="mt-1"
            value={name}
            disabled={isReadOnly}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("workspace.integrations.webhooks.webhook_name_placeholder")}
          />
        </div>
        <div>
          <Label htmlFor="webhook-url">{t("common.url")}</Label>
          <div className="mt-1 flex">
            <Input
              id="webhook-url"
              type="text"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              readOnly={isReadOnly || isThirdParty}
              className={cn(
                (isReadOnly || isThirdParty) && "cursor-not-allowed bg-slate-100 text-slate-500",
                endpointClass(endpoint.accessible)
              )}
              placeholder={t("workspace.integrations.webhooks.webhook_url_placeholder")}
            />
            {!isReadOnly && (
              <Button
                type="button"
                variant="secondary"
                loading={endpoint.hitting}
                className="ml-2 whitespace-nowrap"
                onClick={() => void endpoint.run({ url, webhookId: webhook.id }, true)}>
                {t("workspace.integrations.webhooks.test_endpoint")}
              </Button>
            )}
          </div>
        </div>
        <div>
          <Label>{t("workspace.integrations.webhooks.triggers")}</Label>
          <TriggerCheckboxGroup
            selected={triggers}
            onToggle={(trigger) => setTriggers((current) => toggled(current, trigger))}
            allowChanges={allowChanges}
          />
        </div>
        <div>
          <Label>{t("common.surveys")}</Label>
          <SurveyCheckboxGroup
            surveys={surveys}
            selected={surveyIds}
            allSurveys={allSurveys}
            onToggleAll={() => {
              setAllSurveys((all) => !all);
              setSurveyIds([]);
            }}
            onToggle={(id) => setSurveyIds((current) => toggled(current, id))}
            allowChanges={allowChanges}
          />
        </div>
        <div className="flex justify-between gap-x-2">
          <div className="flex gap-x-2">
            {!isReadOnly && (
              <Button type="button" variant="destructive" onClick={() => setIsDeleteOpen(true)}>
                <TrashIcon />
                {t("common.delete")}
              </Button>
            )}
          </div>
          {!isReadOnly && (
            <Button type="submit" loading={update.isPending}>
              {t("common.save_changes")}
            </Button>
          )}
        </div>
      </form>
      <DeleteDialog
        open={isDeleteOpen}
        setOpen={setIsDeleteOpen}
        deleteWhat={t("common.webhook")}
        text={t("workspace.integrations.webhooks.webhook_delete_confirmation")}
        onDelete={() => void deleteWebhook()}
      />
    </div>
  );
};
