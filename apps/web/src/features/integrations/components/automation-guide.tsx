import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { ExternalLinkIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";

/** The automation platforms the integrations page lists. None has a published Forma app, so each is connected with its own webhook trigger and a Forma webhook. */
export type AutomationPlatform = "zapier" | "make" | "n8n" | "activepieces";

/** The platform's product name, which is not translated, and its own documentation of the webhook trigger the guide uses. */
export const AUTOMATION_PLATFORMS: Record<AutomationPlatform, { name: string; docsUrl: string }> = {
  zapier: {
    name: "Zapier",
    docsUrl: "https://help.zapier.com/hc/en-us/articles/8496288690317-Trigger-Zap-workflows-from-webhooks",
  },
  make: { name: "Make", docsUrl: "https://apps.make.com/gateway" },
  n8n: { name: "n8n", docsUrl: "https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.webhook" },
  activepieces: { name: "Activepieces", docsUrl: "https://www.activepieces.com/pieces/webhook" },
};

/** The steps on the platform's side, all done before Forma's Add Webhook: create its webhook trigger, copy the URL it listens on, and, where that URL only answers once the workflow is live, make it live. Add Webhook tests the URL and refuses one that answers 404, which n8n's Production URL does until the workflow is active. The keys are written out so the catalogue check sees every one. */
const platformSteps = (t: TFunction, platform: AutomationPlatform): string[] => {
  switch (platform) {
    case "zapier":
      return [
        t("workspace.integrations.automation_guide_zapier_step_create"),
        t("workspace.integrations.automation_guide_zapier_step_copy"),
      ];
    case "make":
      return [
        t("workspace.integrations.automation_guide_make_step_create"),
        t("workspace.integrations.automation_guide_make_step_copy"),
      ];
    case "n8n":
      return [
        t("workspace.integrations.automation_guide_n8n_step_create"),
        t("workspace.integrations.automation_guide_n8n_step_copy"),
        t("workspace.integrations.automation_guide_n8n_step_activate"),
      ];
    case "activepieces":
      return [
        t("workspace.integrations.automation_guide_activepieces_step_create"),
        t("workspace.integrations.automation_guide_activepieces_step_copy"),
        t("workspace.integrations.automation_guide_activepieces_step_publish"),
      ];
  }
};

/** How to send responses to an automation platform without a Forma app on it: the platform's webhook trigger, a Forma webhook pointing at it, and a response to give the platform a sample. */
export const AutomationGuideDialog = ({
  platform,
  webhooksPath,
  onClose,
}: {
  platform: AutomationPlatform;
  /** The workspace's webhooks page, where the Forma side is set up. */
  webhooksPath: string;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const { name, docsUrl } = AUTOMATION_PLATFORMS[platform];
  const steps = [
    ...platformSteps(t, platform),
    t("workspace.integrations.automation_guide_step_add_webhook"),
    t("workspace.integrations.automation_guide_step_send_sample", { platform: name }),
  ];

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <DialogTitle>{t("workspace.integrations.automation_guide_title", { platform: name })}</DialogTitle>
        <DialogDescription>
          {t("workspace.integrations.automation_guide_description", { platform: name })}
        </DialogDescription>
      </DialogHeader>
      <DialogBody className="space-y-4 pb-2">
        <ol className="list-decimal space-y-2 pl-5 text-slate-700">
          {steps.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
        <a
          href={docsUrl}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-sm text-slate-600 underline underline-offset-2 hover:text-slate-900">
          {t("workspace.integrations.automation_guide_docs_link", { platform: name })}
          <ExternalLinkIcon className="size-3.5" aria-hidden />
        </a>
      </DialogBody>
      <DialogFooter>
        <Button type="button" variant="secondary" onClick={onClose}>
          {t("common.close")}
        </Button>
        <Button asChild>
          <Link to={webhooksPath}>{t("workspace.integrations.automation_guide_open_webhooks")}</Link>
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
