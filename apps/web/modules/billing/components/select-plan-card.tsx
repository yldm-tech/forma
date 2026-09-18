"use client";

import { CheckIcon, GiftIcon, XCircleIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import toast from "react-hot-toast";
import { useTranslation } from "react-i18next";
import { capturePostHogClientEvent } from "@/lib/posthog/client";
import { startHobbyAction, startProTrialAction } from "@/modules/billing/actions";
import { Button } from "@/modules/ui/components/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/modules/ui/components/dialog";

interface SelectPlanCardProps {
  nextUrl: string;
  organizationId: string;
  trialDays: number;
}

export const SelectPlanCard = ({ nextUrl, organizationId, trialDays }: Readonly<SelectPlanCardProps>) => {
  const router = useRouter();
  const [isStartingTrial, setIsStartingTrial] = useState(false);
  const [isStartingHobby, setIsStartingHobby] = useState(false);
  const [showHobbyConfirm, setShowHobbyConfirm] = useState(false);
  const { t } = useTranslation();

  const copy = {
    header: t("workspace.settings.billing.select_plan_header", { count: trialDays }),
    subheader: t("workspace.settings.billing.select_plan_subheader"),
    cta: t("workspace.settings.billing.select_plan_cta"),
    skip: t("workspace.settings.billing.select_plan_skip"),
  };

  const SELECT_PLAN_FEATURE_KEYS = [
    t("workspace.settings.billing.select_plan_feature_1"),
    t("workspace.settings.billing.select_plan_feature_2"),
    t("workspace.settings.billing.select_plan_feature_3"),
    t("workspace.settings.billing.select_plan_feature_4"),
  ];

  const handleStartTrial = async () => {
    setIsStartingTrial(true);
    try {
      const result = await startProTrialAction({ organizationId });
      if (result?.data) {
        router.push(nextUrl);
      } else if (result?.serverError === "trial_already_used") {
        toast.error(t("workspace.settings.billing.trial_already_used"));
        setIsStartingTrial(false);
      } else {
        toast.error(t("workspace.settings.billing.failed_to_start_trial"));
        setIsStartingTrial(false);
      }
    } catch {
      toast.error(t("workspace.settings.billing.failed_to_start_trial"));
      setIsStartingTrial(false);
    }
  };

  const handleContinueHobby = async () => {
    setIsStartingHobby(true);
    try {
      const result = await startHobbyAction({ organizationId });
      if (result?.data) {
        router.push(nextUrl);
      } else {
        toast.error(t("common.something_went_wrong_please_try_again"));
        setIsStartingHobby(false);
      }
    } catch {
      toast.error(t("common.something_went_wrong_please_try_again"));
      setIsStartingHobby(false);
    }
  };

  return (
    <div className="flex w-full max-w-md flex-col items-center gap-y-6">
      <div className="relative w-full overflow-hidden rounded-xl border border-slate-200 bg-white shadow-lg">
        <div className="flex flex-col items-center gap-y-6 p-8">
          <div className="rounded-full bg-slate-100 p-4">
            <GiftIcon className="size-10 text-slate-600" />
          </div>

          <div className="text-center">
            <h3 className="text-2xl font-semibold text-slate-800">{copy.header}</h3>
            <p className="mt-2 text-slate-600">{copy.subheader}</p>
          </div>

          <ul className="my-3 w-full space-y-3 text-left">
            {SELECT_PLAN_FEATURE_KEYS.map((key) => (
              <li key={key} className="flex items-center gap-3 text-slate-700">
                <CheckIcon className="size-5 shrink-0 text-slate-900" />
                <span>{key}</span>
              </li>
            ))}
          </ul>

          <Button
            size="lg"
            onClick={handleStartTrial}
            className="mt-4 w-full"
            loading={isStartingTrial}
            disabled={isStartingTrial || isStartingHobby}>
            {copy.cta}
          </Button>
        </div>
      </div>

      <button
        type="button"
        onClick={() => setShowHobbyConfirm(true)}
        disabled={isStartingTrial || isStartingHobby}
        className="text-sm text-slate-400 underline-offset-2 transition-colors hover:text-slate-600 hover:underline">
        {copy.skip}
      </button>

      <Dialog open={showHobbyConfirm} onOpenChange={setShowHobbyConfirm}>
        <DialogContent width="narrow" hideCloseButton>
          <DialogHeader>
            <DialogTitle>{t("workspace.settings.billing.hobby_confirm_title")}</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-slate-500">{t("workspace.settings.billing.hobby_confirm_description")}</p>
            <ul className="mt-4 space-y-1.5">
              {[
                { from: "2,000", to: t("workspace.settings.billing.hobby_confirm_feature_responses") },
                { from: "3", to: t("workspace.settings.billing.hobby_confirm_feature_workspaces") },
              ].map((item) => (
                <li key={item.to} className="flex items-center gap-2 text-sm text-slate-700">
                  <XCircleIcon className="size-3.5 shrink-0 text-primary" />
                  <span>
                    <span className="text-slate-400 line-through">{item.from}</span>{" "}
                    <span aria-hidden="true">→</span> {item.to}
                  </span>
                </li>
              ))}
              {[
                t("workspace.settings.billing.hobby_confirm_feature_branding"),
                t("workspace.settings.billing.hobby_confirm_feature_contacts"),
                t("workspace.settings.billing.hobby_confirm_feature_ai"),
              ].map((item) => (
                <li key={item} className="flex items-center gap-2 text-sm text-slate-700">
                  <XCircleIcon className="size-3.5 shrink-0 text-primary" />
                  <span className="text-slate-400 line-through">{item}</span>
                </li>
              ))}
            </ul>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="secondary"
              loading={isStartingHobby}
              onClick={() => {
                capturePostHogClientEvent("billing_onboarding_hobby_confirm_cta_clicked", {
                  cta: "downgrade_hobby",
                });
                void handleContinueHobby();
              }}>
              {t("workspace.settings.billing.hobby_confirm_downgrade")}
            </Button>
            <Button
              onClick={() => {
                capturePostHogClientEvent("billing_onboarding_hobby_confirm_cta_clicked", {
                  cta: "start_trial",
                });
                setShowHobbyConfirm(false);
                void handleStartTrial();
              }}>
              {t("workspace.settings.billing.hobby_confirm_start_trial")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};
