import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLinkIcon, FilesIcon, Trash2Icon, TrashIcon } from "lucide-react";
import { type FormEvent, useId, useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { Dialog, DialogBody, DialogFooter, DialogHeader, DialogTitle } from "../../components/ui/dialog";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { LoadingBlock, PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { Switch } from "../../components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import {
  API_KEY_EXPIRY_CHOICES,
  LAST_CUSTOM_EXPIRY_DAY,
  type ApiKeyExpiryChoice,
  type PermissionRow,
  apiKeyExpiry,
  apiKeyExpiryState,
  apiKeyScope,
  canSubmitApiKey,
  effectiveAccess,
  firstCustomExpiryDay,
  hasDuplicateWorkspace,
  maskedApiKey,
} from "../../lib/api-key-form";
import { copyText } from "../../lib/clipboard";
import { errorMessage } from "../../lib/errors";
import {
  type ApiKey,
  type ApiKeyDisabledReason,
  type ApiKeyPermission,
  type OrganizationAccess,
  apiKeysQuery,
  useCreateApiKey,
  useDeleteApiKey,
  useRelabelApiKey,
} from "../../lib/queries/api-keys";
import type { OrganizationDetail } from "../../lib/queries/organizations";
import { type Workspace, workspacesQuery } from "../../lib/queries/workspaces";
import { accessFlags } from "../../lib/roles";
import { apiReferenceUrl } from "../../lib/sdk-snippets";
import { timeSince } from "../../lib/time";
import { toast } from "../../lib/toast";

const PERMISSIONS: ApiKeyPermission[] = ["read", "write", "manage"];
const NO_ACCESS: OrganizationAccess = { accessControl: { read: false, write: false } };

/** A workspace permission as the pages name it, in the user's language; the enum value itself is never shown. Write includes read on the server, so it reads as the teams page's "Read & Write". */
export const permissionLabel = (t: TFunction, permission: ApiKeyPermission): string => {
  switch (permission) {
    case "read":
      return t("workspace.settings.teams.read");
    case "write":
      return t("workspace.settings.teams.read_write");
    case "manage":
      return t("workspace.settings.teams.manage");
  }
};

/** Why a key was disabled, in the user's language. */
export const disabledReasonLabel = (t: TFunction, reason: ApiKeyDisabledReason): string => {
  switch (reason) {
    case "creator_removed":
      return t("workspace.api_keys.disabled_reason_creator_removed");
    case "creator_left":
      return t("workspace.api_keys.disabled_reason_creator_left");
    case "creator_deleted":
      return t("workspace.api_keys.disabled_reason_creator_deleted");
    case "creator_demoted":
      return t("workspace.api_keys.disabled_reason_creator_demoted");
  }
};

/** An expiry the create form offers, in the user's language. */
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

/** A key's state beside its label: disabled (which wins, since it cannot be undone), expired, or expiring within a week. Nothing for a key that works and will for a while. */
const KeyStatusBadge = ({ apiKey, formatDate }: { apiKey: ApiKey; formatDate: Intl.DateTimeFormat }) => {
  const { t } = useTranslation();
  if (apiKey.disabledReason) {
    return (
      <Badge
        type="error"
        size="tiny"
        text={t("common.disabled")}
        title={disabledReasonLabel(t, apiKey.disabledReason)}
      />
    );
  }
  const state = apiKeyExpiryState(apiKey);
  if (!apiKey.expiresAt || (state !== "expired" && state !== "soon")) return null;
  const date = formatDate.format(new Date(apiKey.expiresAt));
  return state === "expired" ? (
    <Badge
      type="error"
      size="tiny"
      text={t("common.expired")}
      title={t("workspace.api_keys.expired_on", { date })}
    />
  ) : (
    <Badge
      type="warning"
      size="tiny"
      text={t("workspace.api_keys.expires_soon")}
      title={t("workspace.api_keys.expires_on", { date })}
    />
  );
};

/** What a key may reach, in one line: the first workspace grants by name, how many more, and its organization access. */
export const scopeSummary = (
  t: TFunction,
  key: Pick<ApiKey, "workspacePermissions" | "organizationAccess">
): string => {
  const scope = apiKeyScope(key);
  const parts = scope.shown.map((grant) =>
    t("workspace.api_keys.scope_workspace", {
      workspace: grant.workspaceName,
      permission: permissionLabel(t, grant.permission),
    })
  );
  if (scope.more > 0) parts.push(t("workspace.api_keys.scope_more", { count: scope.more }));
  if (scope.organization) {
    parts.push(
      t("workspace.api_keys.scope_organization", { access: permissionLabel(t, scope.organization) })
    );
  }
  return parts.length > 0 ? parts.join(" · ") : t("workspace.api_keys.scope_none");
};

/** The API reference on the website (REST API v3 and MCP), from the page header. */
const ApiReferenceLink = () => {
  const { t, i18n } = useTranslation();
  return (
    <Button asChild variant="secondary" size="sm">
      <a href={apiReferenceUrl(i18n.language)} target="_blank" rel="noopener noreferrer">
        {t("workspace.api_keys.api_reference")}
        <ExternalLinkIcon className="ml-1 size-4" aria-hidden="true" />
      </a>
    </Button>
  );
};

/** forma2's API keys page: owners and managers (organization.manage_api_keys) list, create, relabel and revoke the organization's keys. */
export const ApiKeysPage = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  if (!accessFlags(organization.membership.role).isOwnerOrManager) {
    return (
      <PageContentWrapper>
        <PageHeader pageTitle={t("common.api_keys")} />
        <Alert variant="warning" role="status">
          <AlertDescription>{t("common.you_are_not_authorized_to_perform_this_action")}</AlertDescription>
        </Alert>
      </PageContentWrapper>
    );
  }
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.api_keys")} cta={<ApiReferenceLink />} />
      <ConnectAppBanner organizationId={organization.id} />
      <SettingsCard
        title={t("common.api_keys")}
        description={t("workspace.settings.api_keys.api_keys_description")}
        bodyVariant="flush"
        width="full">
        <ApiKeyList organizationId={organization.id} />
      </SettingsCard>
    </PageContentWrapper>
  );
};

/** forma2's pointer for someone looking for a key to set up a website or app survey, which needs none: the first workspace's SDK page. Shown while the organization has a workspace. */
export const ConnectAppBanner = ({ organizationId }: { organizationId: string }) => {
  const { t } = useTranslation();
  const { data: workspaces } = useQuery(workspacesQuery(organizationId));
  const first = workspaces?.[0];
  if (!first) return null;
  return (
    <Alert variant="info" role="status" className="rounded-xl">
      <AlertTitle>{t("workspace.settings.api_keys.connect_app_banner_title")}</AlertTitle>
      <AlertDescription>{t("workspace.settings.api_keys.connect_app_banner_description")}</AlertDescription>
      <AlertButton asChild>
        <Link to="/workspaces/$workspaceId/integrations/app-connection" params={{ workspaceId: first.id }}>
          {t("workspace.settings.api_keys.connect_app_banner_link")}
        </Link>
      </AlertButton>
    </Alert>
  );
};

const ApiKeyList = ({ organizationId }: { organizationId: string }) => {
  const { t, i18n } = useTranslation();
  const keys = useQuery(apiKeysQuery(organizationId));
  const workspaces = useQuery(workspacesQuery(organizationId));
  const [addOpen, setAddOpen] = useState(false);
  const [viewed, setViewed] = useState<ApiKey | null>(null);
  const [deleting, setDeleting] = useState<ApiKey | null>(null);
  // The plaintext of keys created on this visit, by id. The server returns it once; it lives only in this component's memory and is gone on reload.
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const remove = useDeleteApiKey(organizationId);
  const formatDate = new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" });
  const formatDay = new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium" });
  // The keys created on this visit that are still listed: their plaintext, and the v3/MCP note about them, go away once none of them is listed any more, for instance after the user deletes the one they just created.
  const createdKeys = keys.data?.filter((key) => key.id in secrets) ?? [];

  const deleteKey = async () => {
    if (!deleting) return;
    try {
      await remove.mutateAsync(deleting.id);
      toast.success(t("workspace.api_keys.api_key_deleted"));
    } catch {
      toast.error(t("workspace.api_keys.unable_to_delete_api_key"));
    }
    setDeleting(null);
  };

  return (
    <>
      <div className="mb-4 flex justify-end px-4 pt-4">
        <Button size="sm" onClick={() => setAddOpen(true)}>
          {t("workspace.settings.api_keys.add_api_key")}
        </Button>
      </div>
      {createdKeys.length > 0 && <NewKeyNotice keys={createdKeys} secrets={secrets} />}
      {keys.isPending ? (
        <div className="px-4 py-4">
          <LoadingBlock />
        </div>
      ) : keys.isError ? (
        <p role="alert" className="px-4 py-6 text-sm text-red-600">
          {errorMessage(t, keys.error)}
        </p>
      ) : keys.data.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-slate-500">
          {t("workspace.api_keys.no_api_keys_yet")}
        </p>
      ) : (
        <Table aria-label={t("common.api_keys")}>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[22%]">{t("common.label")}</TableHead>
              <TableHead className="hidden w-[24%] md:table-cell">{t("workspace.api_keys.access")}</TableHead>
              <TableHead className="hidden w-[15%] sm:table-cell">{t("common.created_by")}</TableHead>
              <TableHead className="w-[13%]">{t("workspace.api_keys.last_used")}</TableHead>
              <TableHead className="hidden w-[10%] lg:table-cell">
                {t("workspace.api_keys.expires")}
              </TableHead>
              <TableHead className="hidden w-[11%] xl:table-cell">{t("common.created_at")}</TableHead>
              <TableHead className="w-[5%]">
                <span className="sr-only">{t("common.actions")}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {keys.data.map((key) => (
              <TableRow key={key.id} data-testid="api-key-row">
                <TableCell className="font-semibold">
                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      className="text-left hover:underline"
                      aria-label={t("workspace.api_keys.view_permissions_for", { label: key.label })}
                      onClick={() => setViewed(key)}>
                      {key.label}
                    </button>
                    <KeyStatusBadge apiKey={key} formatDate={formatDate} />
                  </div>
                  <span className="block font-mono text-xs font-normal text-slate-500">
                    {maskedApiKey(key.keyHint)}
                  </span>
                </TableCell>
                <TableCell className="hidden text-sm text-slate-600 md:table-cell">
                  {scopeSummary(t, key)}
                </TableCell>
                <TableCell className="hidden sm:table-cell">
                  <CreatedBy createdBy={key.createdBy} />
                </TableCell>
                <TableCell className="text-sm text-slate-600">
                  {key.lastUsedAt ? (
                    <time dateTime={key.lastUsedAt} title={formatDate.format(new Date(key.lastUsedAt))}>
                      {timeSince(key.lastUsedAt, i18n.language)}
                    </time>
                  ) : (
                    t("workspace.api_keys.never_used")
                  )}
                </TableCell>
                <TableCell className="hidden text-sm text-slate-600 lg:table-cell">
                  {key.expiresAt ? (
                    <time dateTime={key.expiresAt}>{formatDay.format(new Date(key.expiresAt))}</time>
                  ) : (
                    t("workspace.api_keys.never_expires")
                  )}
                </TableCell>
                <TableCell className="hidden xl:table-cell">
                  {formatDate.format(new Date(key.createdAt))}
                </TableCell>
                <TableCell>
                  <div className="flex justify-end">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={t("common.delete")}
                      onClick={() => setDeleting(key)}>
                      <TrashIcon />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <AddApiKeyModal
        open={addOpen}
        setOpen={setAddOpen}
        organizationId={organizationId}
        workspaces={workspaces.data ?? []}
        onCreated={(id, key) => setSecrets((current) => ({ ...current, [id]: key }))}
      />
      {viewed && (
        <ViewPermissionModal
          apiKey={viewed}
          organizationId={organizationId}
          onClose={() => setViewed(null)}
        />
      )}
      <DeleteDialog
        open={deleting !== null}
        setOpen={(open) => !open && setDeleting(null)}
        deleteWhat={t("workspace.api_keys.api_key")}
        onDelete={() => void deleteKey()}
        isDeleting={remove.isPending}
        text={t("workspace.api_keys.delete_api_key_confirmation")}
      />
    </>
  );
};

/** The keys created on this visit, with their plaintext: the server returns it once, so it is shown here, to the browser that created it, until it leaves the page or deletes the key. */
const NewKeyNotice = ({ keys, secrets }: { keys: ApiKey[]; secrets: Record<string, string> }) => {
  const { t } = useTranslation();
  return (
    <div className="mx-4 mb-4 space-y-3 rounded-xl border border-green-200 bg-green-50 p-4">
      <div>
        <p className="text-sm font-semibold text-slate-800">{t("workspace.api_keys.new_key_title")}</p>
        <p className="text-sm text-slate-600">{t("workspace.api_keys.new_key_description")}</p>
      </div>
      <ul className="space-y-2">
        {keys.map((key) => (
          <li key={key.id} className="space-y-1">
            <p className="text-sm font-medium text-slate-700">{key.label}</p>
            <ApiKeyDisplay secret={secrets[key.id] ?? ""} />
          </li>
        ))}
      </ul>
      <p role="status" className="text-sm text-slate-600">
        {t("workspace.api_keys.v3_propagation_note")}
      </p>
      <div className="space-y-1">
        <p className="text-sm text-slate-600">{t("workspace.api_keys.check_key_hint")}</p>
        <CommandDisplay command={checkKeyCommand(window.location.origin)} />
        <p className="text-sm text-slate-600">
          {t("workspace.api_keys.openapi_document_hint")}{" "}
          <a
            href={openAPIDocumentUrl(window.location.origin)}
            target="_blank"
            rel="noopener noreferrer"
            className="font-mono text-xs break-all text-slate-800 underline">
            {openAPIDocumentUrl(window.location.origin)}
          </a>
        </p>
      </div>
    </div>
  );
};

/** The command that asks the API which organization and workspaces a key reaches. The key stays in an environment variable, out of the shell history; the hint above it says to set the variable first. */
const checkKeyCommand = (origin: string): string => `curl -H "x-api-key: $FORMA_API_KEY" ${origin}/api/v3/me`;

/** The instance's OpenAPI document of the v3 API, which needs no credentials. */
const openAPIDocumentUrl = (origin: string): string => `${origin}/api/v3/openapi.json`;

/** A shell command with a copy button, laid out like ApiKeyDisplay. */
const CommandDisplay = ({ command }: { command: string }) => {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-between gap-2 rounded-md border border-slate-200 bg-white px-3 py-2">
      <code className="font-mono text-xs break-all">{command}</code>
      <button
        type="button"
        className="shrink-0 text-slate-500 hover:text-slate-800"
        aria-label={t("common.copy")}
        onClick={() => void copyText(command, t)}>
        <FilesIcon className="size-4" />
      </button>
    </div>
  );
};

/** A plaintext key with a copy button. */
const ApiKeyDisplay = ({ secret }: { secret: string }) => {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-between gap-2 rounded-md border border-slate-200 bg-white px-3 py-2">
      <span className="font-mono text-sm break-all">{secret}</span>
      <button
        type="button"
        className="copyApiKeyIcon shrink-0 text-slate-500 hover:text-slate-800"
        aria-label={t("common.copy")}
        onClick={() => void copyText(secret, t, t("workspace.api_keys.api_key_copied_to_clipboard"))}>
        <FilesIcon className="size-4" />
      </button>
    </div>
  );
};

/** Who created a key. A creator who no longer belongs to the organization, or whose account is gone, reads as a former member, so an owner can find keys left behind. A suspended creator is marked too: their keys do not work until their access is restored; so is one whose account the instance deactivated, until it is turned back on. */
const CreatedBy = ({ createdBy }: { createdBy: ApiKey["createdBy"] }) => {
  const { t } = useTranslation();
  if (createdBy?.deactivated)
    return (
      <span className="flex flex-wrap items-center gap-1 text-sm">
        <span>{createdBy.name}</span>
        <Badge
          type="gray"
          size="tiny"
          text={t("common.inactive")}
          title={t("workspace.api_keys.creator_deactivated_hint")}
        />
        <span className="sr-only">{t("workspace.api_keys.creator_deactivated_hint")}</span>
      </span>
    );
  if (createdBy?.suspended)
    return (
      <span className="flex flex-wrap items-center gap-1 text-sm">
        <span>{createdBy.name}</span>
        <Badge
          type="error"
          size="tiny"
          text={t("organizations.members.suspended")}
          title={t("organizations.members.creator_suspended_hint")}
        />
        <span className="sr-only">{t("organizations.members.creator_suspended_hint")}</span>
      </span>
    );
  if (createdBy && !createdBy.formerMember) return <span className="text-sm">{createdBy.name}</span>;
  return (
    <span className="flex flex-wrap items-center gap-1 text-sm text-slate-500">
      {createdBy && <span>{createdBy.name}</span>}
      <Badge type="gray" size="tiny" text={t("workspace.api_keys.former_member")} />
    </span>
  );
};

/** forma2's AddApiKeyModal: a label, workspace grants (each workspace once) and organization access, write implying read. */
const AddApiKeyModal = ({
  open,
  setOpen,
  organizationId,
  workspaces,
  onCreated,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  organizationId: string;
  workspaces: Workspace[];
  onCreated: (id: string, key: string) => void;
}) => {
  const { t, i18n } = useTranslation();
  const [label, setLabel] = useState("");
  const [rows, setRows] = useState<PermissionRow[]>([]);
  const [access, setAccess] = useState<OrganizationAccess>(NO_ACCESS);
  const [expiry, setExpiry] = useState<ApiKeyExpiryChoice>("never");
  const [customDay, setCustomDay] = useState("");
  const create = useCreateApiKey(organizationId);
  const shown = effectiveAccess(access);
  // Shown under the choice; the expiry sent is worked out again on submit, from the time of the submit.
  const expiresAt = apiKeyExpiry(expiry, customDay);
  const formatDate = new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" });

  const reset = () => {
    setLabel("");
    setRows([]);
    setAccess(NO_ACCESS);
    setExpiry("never");
    setCustomDay("");
  };
  const close = () => {
    setOpen(false);
    reset();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (hasDuplicateWorkspace(rows)) {
      toast.error(t("workspace.api_keys.duplicate_access"));
      return;
    }
    const at = apiKeyExpiry(expiry, customDay);
    if (at === undefined) return;
    try {
      const created = await create.mutateAsync({
        label,
        workspacePermissions: rows,
        organizationAccess: shown,
        expiresAt: at ? at.toISOString() : null,
      });
      onCreated(created.id, created.key);
      toast.success(t("workspace.api_keys.api_key_created"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const first = workspaces[0];

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())}>
      <DialogHeader>
        <DialogTitle className="px-1">{t("workspace.api_keys.add_api_key")}</DialogTitle>
      </DialogHeader>
      <form onSubmit={submit} className="contents">
        <DialogBody className="space-y-4 px-1 py-4">
          <div className="space-y-2">
            <Label htmlFor="api-key-label">{t("workspace.api_keys.api_key_label")}</Label>
            <Input
              id="api-key-label"
              placeholder="e.g. GitHub, PostHog, Slack"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
            />
          </div>
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-slate-800">
              {t("workspace.api_keys.workspace_access")}
            </legend>
            <p className="text-sm text-slate-500">{t("workspace.api_keys.workspace_access_description")}</p>
            {rows.length === 0 && (
              <p className="text-sm text-slate-500">{t("workspace.api_keys.workspace_access_empty")}</p>
            )}
            {rows.map((row, index) => (
              // oxlint-disable-next-line react/no-array-index-key -- grant rows are positional
              <div key={index} className="flex items-center gap-2">
                <div className="w-1/2">
                  <Select
                    aria-label={t("common.workspace")}
                    value={row.workspaceId}
                    onChange={(e) =>
                      setRows(rows.map((r, i) => (i === index ? { ...r, workspaceId: e.target.value } : r)))
                    }>
                    {workspaces.map((workspace) => (
                      <option key={workspace.id} value={workspace.id}>
                        {workspace.name}
                      </option>
                    ))}
                  </Select>
                </div>
                <div className="w-1/2">
                  <Select
                    aria-label={t("workspace.api_keys.permissions")}
                    value={row.permission}
                    onChange={(e) =>
                      setRows(
                        rows.map((r, i) =>
                          i === index ? { ...r, permission: e.target.value as ApiKeyPermission } : r
                        )
                      )
                    }>
                    {PERMISSIONS.map((permission) => (
                      <option key={permission} value={permission}>
                        {permissionLabel(t, permission)}
                      </option>
                    ))}
                  </Select>
                </div>
                <button
                  type="button"
                  className="p-2"
                  aria-label={t("common.remove")}
                  onClick={() => setRows(rows.filter((_, i) => i !== index))}>
                  <Trash2Icon className="size-5 text-slate-500 hover:text-red-500" />
                </button>
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              disabled={!first}
              onClick={() => first && setRows([...rows, { workspaceId: first.id, permission: "read" }])}>
              <span className="mr-2">+</span> {t("workspace.settings.api_keys.add_permission")}
            </Button>
          </fieldset>
          <div className="border-t border-slate-200" />
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-slate-800">
              {t("workspace.api_keys.organization_access")}
            </legend>
            <p className="text-sm text-slate-500">
              {t("workspace.api_keys.organization_access_description")}
            </p>
            <OrganizationAccessSwitches
              access={shown}
              onChange={(next) => setAccess(next)}
              readDisabled={access.accessControl.write}
            />
          </fieldset>
          <div className="border-t border-slate-200" />
          <div className="space-y-2">
            <Label htmlFor="api-key-expiry">{t("workspace.api_keys.expiration")}</Label>
            <Select
              id="api-key-expiry"
              value={expiry}
              onChange={(e) => setExpiry(e.target.value as ApiKeyExpiryChoice)}>
              {API_KEY_EXPIRY_CHOICES.map((choice) => (
                <option key={choice} value={choice}>
                  {expiryChoiceLabel(t, choice)}
                </option>
              ))}
            </Select>
            {expiry === "custom" && (
              <Input
                type="date"
                aria-label={t("workspace.api_keys.expiration_date")}
                min={firstCustomExpiryDay()}
                max={LAST_CUSTOM_EXPIRY_DAY}
                value={customDay}
                onChange={(e) => setCustomDay(e.target.value)}
              />
            )}
            {expiresAt !== undefined && (
              <p className="text-sm text-slate-500">
                {expiresAt
                  ? t("workspace.api_keys.expiration_description", { date: formatDate.format(expiresAt) })
                  : t("workspace.api_keys.expiration_never_description")}
              </p>
            )}
          </div>
          <Alert variant="warning" role="status">
            <AlertTitle>{t("workspace.api_keys.api_key_security_warning")}</AlertTitle>
          </Alert>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            disabled={!canSubmitApiKey(label, rows, access) || expiresAt === undefined}
            loading={create.isPending}>
            {t("workspace.api_keys.add_api_key")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};

const OrganizationAccessSwitches = ({
  access,
  onChange,
  readDisabled,
  disabled = false,
}: {
  access: OrganizationAccess;
  onChange?: (access: OrganizationAccess) => void;
  readDisabled?: boolean;
  disabled?: boolean;
}) => {
  const { t } = useTranslation();
  const id = useId();
  return (
    <div className="mt-2 flex items-center gap-6">
      <div className="flex items-center gap-2">
        <span id={`${id}-read`} className="text-sm font-medium text-slate-800">
          {t("workspace.api_keys.organization_read")}
        </span>
        <Switch
          aria-labelledby={`${id}-read`}
          checked={access.accessControl.read}
          disabled={disabled || readDisabled}
          onCheckedChange={(read) => onChange?.({ accessControl: { ...access.accessControl, read } })}
        />
      </div>
      <div className="flex items-center gap-2">
        <span id={`${id}-write`} className="text-sm font-medium text-slate-800">
          {t("workspace.api_keys.organization_write")}
        </span>
        <Switch
          aria-labelledby={`${id}-write`}
          checked={access.accessControl.write}
          disabled={disabled}
          onCheckedChange={(write) => onChange?.({ accessControl: { ...access.accessControl, write } })}
        />
      </div>
    </div>
  );
};

/** forma2's ViewPermissionModal: a key's grants, read-only, and its label, which may be changed. */
const ViewPermissionModal = ({
  apiKey,
  organizationId,
  onClose,
}: {
  apiKey: ApiKey;
  organizationId: string;
  onClose: () => void;
}) => {
  const { t, i18n } = useTranslation();
  const [label, setLabel] = useState(apiKey.label);
  const relabel = useRelabelApiKey(organizationId);
  const formatDate = new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" });
  const unchanged = label.trim() === "" || label === apiKey.label;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (unchanged) return;
    try {
      await relabel.mutateAsync({ id: apiKey.id, label });
      toast.success(t("workspace.api_keys.api_key_updated"));
      onClose();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <DialogTitle>{apiKey.label}</DialogTitle>
      </DialogHeader>
      <form onSubmit={submit} className="contents">
        <DialogBody className="space-y-6">
          {apiKey.disabledReason && apiKey.disabledAt ? (
            <Alert variant="error" role="status">
              <AlertTitle>{disabledReasonLabel(t, apiKey.disabledReason)}</AlertTitle>
              <AlertDescription>
                {t("workspace.api_keys.disabled_description", {
                  date: formatDate.format(new Date(apiKey.disabledAt)),
                })}
              </AlertDescription>
            </Alert>
          ) : (
            apiKey.expiresAt &&
            apiKeyExpiryState(apiKey) === "expired" && (
              <Alert variant="error" role="status">
                <AlertTitle>
                  {t("workspace.api_keys.expired_on", {
                    date: formatDate.format(new Date(apiKey.expiresAt)),
                  })}
                </AlertTitle>
                <AlertDescription>{t("workspace.api_keys.expired_description")}</AlertDescription>
              </Alert>
            )
          )}
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
            <dt className="font-medium text-slate-800">{t("workspace.api_keys.api_key")}</dt>
            <dd className="font-mono text-slate-600">{maskedApiKey(apiKey.keyHint)}</dd>
            <dt className="font-medium text-slate-800">{t("common.created_by")}</dt>
            <dd>
              <CreatedBy createdBy={apiKey.createdBy} />
            </dd>
            <dt className="font-medium text-slate-800">{t("workspace.api_keys.last_used")}</dt>
            <dd className="text-slate-600">
              {apiKey.lastUsedAt
                ? formatDate.format(new Date(apiKey.lastUsedAt))
                : t("workspace.api_keys.never_used")}
            </dd>
            <dt className="font-medium text-slate-800">{t("workspace.api_keys.expires")}</dt>
            <dd className="text-slate-600">
              {apiKey.expiresAt
                ? formatDate.format(new Date(apiKey.expiresAt))
                : t("workspace.api_keys.never_expires")}
            </dd>
          </dl>
          <div className="space-y-2">
            <Label htmlFor="api-key-relabel">{t("workspace.api_keys.api_key_label")}</Label>
            <Input
              id="api-key-relabel"
              data-testid="api-key-label"
              placeholder="e.g. GitHub, PostHog, Slack"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <p className="text-sm font-medium text-slate-800">{t("workspace.api_keys.permissions")}</p>
            {apiKey.workspacePermissions.length === 0 ? (
              <p className="text-center text-sm">{t("workspace.api_keys.no_workspace_permissions_found")}</p>
            ) : (
              <ul className="space-y-2">
                {apiKey.workspacePermissions.map((permission) => (
                  <li key={permission.workspaceId} className="flex items-center gap-2 text-sm">
                    <span className="w-1/2 truncate rounded-md border border-slate-300 px-3 py-2">
                      {permission.workspaceName}
                    </span>
                    <span className="w-1/2 truncate rounded-md border border-slate-300 px-3 py-2">
                      {permissionLabel(t, permission.permission)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="space-y-2">
            <p className="text-sm font-medium text-slate-800">
              {t("workspace.api_keys.organization_access")}
            </p>
            <OrganizationAccessSwitches access={effectiveAccess(apiKey.organizationAccess)} disabled />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={unchanged} loading={relabel.isPending}>
            {t("common.update")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
