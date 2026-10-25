import { useQuery } from "@tanstack/react-query";
import { CheckIcon, CopyIcon, KeyRoundIcon, TrashIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { ConfirmationModal } from "../../components/ui/confirmation-modal";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { MultiSelect } from "../../components/ui/multi-select";
import { LoadingBlock, SettingsCard } from "../../components/ui/page";
import { Switch } from "../../components/ui/switch";
import { attributeKeyLabel } from "../../lib/attribute-keys";
import { copyText } from "../../lib/clipboard";
import { errorMessage } from "../../lib/errors";
import { attributeKeysQuery } from "../../lib/queries/contacts";
import {
  type SdkIdentity,
  type WorkspaceDetail,
  sdkIdentityQuery,
  useRemoveSdkIdentitySecret,
  useRotateSdkIdentitySecret,
  useUpdateSdkIdentity,
} from "../../lib/queries/workspaces";
import { identityVerificationSnippets } from "../../lib/sdk-snippets";
import { toast } from "../../lib/toast";
import { CodeBlock } from "./code-block";

/** The keys that cannot be protected: userId names the contact being identified, and email has its own rule (verified identifies only). */
const UNPROTECTABLE_KEYS = ["userId", "email"];

/** How far the SDK is trusted with the workspace's contacts: the identity secret a customer's server signs user ids with, whether every identify must be verified, and the attributes the SDK never writes. Managers change it; everyone else who can read the workspace sees it. */
export const SdkIdentityCard = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const identity = useQuery(sdkIdentityQuery(workspace.id));
  return (
    <SettingsCard
      width="full"
      title={t("workspace.sdk_identity.title")}
      description={t("workspace.sdk_identity.description")}>
      <div className="pb-4">
        {identity.isPending ? (
          <LoadingBlock />
        ) : identity.isError ? (
          <div className="space-y-2">
            <p role="alert" className="text-sm text-slate-600">
              {errorMessage(t, identity.error)}
            </p>
            <Button variant="secondary" size="sm" onClick={() => void identity.refetch()}>
              {t("common.try_again")}
            </Button>
          </div>
        ) : (
          <SdkIdentitySettings
            workspaceId={workspace.id}
            identity={identity.data}
            canManage={workspace.access.canManage}
          />
        )}
      </div>
    </SettingsCard>
  );
};

const SdkIdentitySettings = ({
  workspaceId,
  identity,
  canManage,
}: {
  workspaceId: string;
  identity: SdkIdentity;
  canManage: boolean;
}) => {
  const { t } = useTranslation();
  const update = useUpdateSdkIdentity(workspaceId);
  const keys = useQuery(attributeKeysQuery(workspaceId));

  const save = async (input: { required?: boolean; protectedAttributeKeys?: string[] }) => {
    try {
      await update.mutateAsync(input);
      toast.success(t("workspace.sdk_identity.saved"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  // The workspace's keys that can be protected, plus any protected key that no longer exists, so it can still be removed.
  const options = (keys.data ?? [])
    .filter((key) => !UNPROTECTABLE_KEYS.includes(key.key))
    .map((key) => ({ value: key.key, label: attributeKeyLabel(key) }));
  for (const key of identity.protectedAttributeKeys) {
    if (!options.some((option) => option.value === key)) options.push({ value: key, label: key });
  }

  return (
    <div className="space-y-6">
      <SecretSection workspaceId={workspaceId} identity={identity} canManage={canManage} />

      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <Label htmlFor="sdk-identity-required">{t("workspace.sdk_identity.required_label")}</Label>
          <p className="text-sm text-slate-500">
            {identity.hasSecret
              ? t("workspace.sdk_identity.required_description")
              : t("workspace.sdk_identity.required_needs_secret")}
          </p>
        </div>
        <Switch
          id="sdk-identity-required"
          checked={identity.required}
          disabled={!canManage || !identity.hasSecret || update.isPending}
          onCheckedChange={(required) => void save({ required })}
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="sdk-identity-protected">{t("workspace.sdk_identity.protected_label")}</Label>
        <p className="text-sm text-slate-500">{t("workspace.sdk_identity.protected_description")}</p>
        <MultiSelect
          id="sdk-identity-protected"
          value={identity.protectedAttributeKeys}
          options={options}
          placeholder={t("workspace.sdk_identity.protected_placeholder")}
          disabled={!canManage || update.isPending}
          onChange={(protectedAttributeKeys) => void save({ protectedAttributeKeys })}
        />
      </div>

      <div className="space-y-2">
        <Label>{t("workspace.sdk_identity.usage_label")}</Label>
        <p className="text-sm text-slate-500">{t("workspace.sdk_identity.usage_description")}</p>
        <CodeBlock>{identityVerificationSnippets.node}</CodeBlock>
        <CodeBlock>{identityVerificationSnippets.python}</CodeBlock>
        <CodeBlock>{identityVerificationSnippets.client}</CodeBlock>
      </div>

      {!canManage && <p className="text-sm text-slate-500">{t("workspace.sdk_identity.read_only")}</p>}
    </div>
  );
};

/** The identity secret: generated, regenerated or removed after a confirmation, and shown only right after it is generated. */
const SecretSection = ({
  workspaceId,
  identity,
  canManage,
}: {
  workspaceId: string;
  identity: SdkIdentity;
  canManage: boolean;
}) => {
  const { t } = useTranslation();
  const rotate = useRotateSdkIdentitySecret(workspaceId);
  const remove = useRemoveSdkIdentitySecret(workspaceId);
  const [confirm, setConfirm] = useState<"rotate" | "remove" | null>(null);
  const [secret, setSecret] = useState<string | null>(null);

  const generate = async () => {
    try {
      setSecret((await rotate.mutateAsync()).secret);
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setConfirm(null);
    }
  };

  const removeSecret = async () => {
    try {
      await remove.mutateAsync();
      toast.success(t("workspace.sdk_identity.saved"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setConfirm(null);
    }
  };

  return (
    <div className="space-y-1">
      <Label>{t("workspace.sdk_identity.secret_label")}</Label>
      <div className="flex items-center justify-between gap-4 rounded-lg border border-slate-200 bg-slate-50 p-3">
        <p className="text-sm text-slate-600">
          {identity.hasSecret
            ? t("workspace.sdk_identity.secret_set")
            : t("workspace.sdk_identity.secret_none")}
        </p>
        {canManage && (
          <div className="flex shrink-0 gap-2">
            {identity.hasSecret ? (
              <>
                <Button type="button" size="sm" variant="secondary" onClick={() => setConfirm("rotate")}>
                  <KeyRoundIcon className="size-4" />
                  {t("workspace.sdk_identity.regenerate")}
                </Button>
                <Button type="button" size="sm" variant="ghost" onClick={() => setConfirm("remove")}>
                  <TrashIcon className="size-4" />
                  {t("workspace.sdk_identity.remove")}
                </Button>
              </>
            ) : (
              <Button
                type="button"
                size="sm"
                variant="secondary"
                loading={rotate.isPending}
                onClick={() => void generate()}>
                <KeyRoundIcon className="size-4" />
                {t("workspace.sdk_identity.generate")}
              </Button>
            )}
          </div>
        )}
      </div>
      <ConfirmationModal
        open={confirm === "rotate"}
        setOpen={(open) => setConfirm(open ? "rotate" : null)}
        title={t("workspace.sdk_identity.regenerate_title")}
        body={<p className="text-sm text-slate-600">{t("workspace.sdk_identity.regenerate_description")}</p>}
        buttonText={t("workspace.sdk_identity.regenerate_confirm")}
        buttonLoading={rotate.isPending}
        onConfirm={() => void generate()}
      />
      <ConfirmationModal
        open={confirm === "remove"}
        setOpen={(open) => setConfirm(open ? "remove" : null)}
        title={t("workspace.sdk_identity.remove_title")}
        body={<p className="text-sm text-slate-600">{t("workspace.sdk_identity.remove_description")}</p>}
        buttonText={t("workspace.sdk_identity.remove_confirm")}
        buttonLoading={remove.isPending}
        onConfirm={() => void removeSecret()}
      />
      {secret !== null && <SecretCreatedModal secret={secret} onClose={() => setSecret(null)} />}
    </div>
  );
};

/** The identity secret, shown this once, with a copy button. */
const SecretCreatedModal = ({ secret, onClose }: { secret: string; onClose: () => void }) => {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <CheckIcon className="size-6 text-green-500" />
        <DialogTitle>{t("workspace.sdk_identity.secret_created_title")}</DialogTitle>
        <DialogDescription>{t("workspace.sdk_identity.secret_created_description")}</DialogDescription>
      </DialogHeader>
      <DialogBody className="space-y-4 pb-4">
        <div>
          <Label htmlFor="sdk-identity-secret">{t("workspace.sdk_identity.secret_label")}</Label>
          <div className="mt-1 flex">
            <Input
              id="sdk-identity-secret"
              type="text"
              readOnly
              value={secret}
              className="font-mono text-sm"
            />
            <Button
              type="button"
              variant="secondary"
              className="ml-2 whitespace-nowrap"
              onClick={() => void copyText(secret, t).then(setCopied)}>
              {copied ? <CheckIcon className="size-4" /> : <CopyIcon className="size-4" />}
              {copied ? t("common.copied") : t("common.copy")}
            </Button>
          </div>
          <p className="mt-2 text-xs text-slate-500">{t("workspace.sdk_identity.secret_created_warning")}</p>
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
