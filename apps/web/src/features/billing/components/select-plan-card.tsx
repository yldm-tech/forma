import { useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CheckIcon, GiftIcon, XCircleIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogBody, DialogFooter, DialogHeader, DialogTitle } from "../../../components/ui/dialog";
import { ApiError } from "../../../lib/api";
import { billingApi, billingSummaryQuery } from "../../../lib/queries/billing";
import { toast } from "../../../lib/toast";

/**
 * forma2's SelectPlanCard, the onboarding plan step on Forma Cloud: start the Pro trial, or stay on Hobby after a confirmation that lists what that gives up. Either way the flow moves on to nextUrl. The trial it promises is the one the server would start, which forma2's a-b_billing_shorten-trial-days experiment shortens for some organizations.
 */
export const SelectPlanCard = ({ organizationId, nextUrl }: { organizationId: string; nextUrl: string }) => {
  const { t } = useTranslation();
  const {
    data: { proTrialDays: trialDays },
  } = useSuspenseQuery(billingSummaryQuery(organizationId));
  const navigate = useNavigate();
  const [isStartingTrial, setIsStartingTrial] = useState(false);
  const [isStartingHobby, setIsStartingHobby] = useState(false);
  const [showHobbyConfirm, setShowHobbyConfirm] = useState(false);

  const startTrial = async () => {
    setIsStartingTrial(true);
    try {
      await billingApi.startProTrial(organizationId);
      await navigate({ href: nextUrl });
    } catch (error) {
      toast.error(
        error instanceof ApiError && error.code === "trial_already_used"
          ? t("workspace.settings.billing.trial_already_used")
          : t("workspace.settings.billing.failed_to_start_trial")
      );
      setIsStartingTrial(false);
    }
  };

  const continueOnHobby = async () => {
    setIsStartingHobby(true);
    try {
      await billingApi.startHobby(organizationId);
      await navigate({ href: nextUrl });
    } catch {
      toast.error(t("common.something_went_wrong_please_try_again"));
      setIsStartingHobby(false);
    }
  };

  const features = [
    t("workspace.settings.billing.select_plan_feature_1"),
    t("workspace.settings.billing.select_plan_feature_2"),
    t("workspace.settings.billing.select_plan_feature_3"),
    t("workspace.settings.billing.select_plan_feature_4"),
  ];
  const reducedLimits = [
    { from: "2,000", to: t("workspace.settings.billing.hobby_confirm_feature_responses") },
    { from: "3", to: t("workspace.settings.billing.hobby_confirm_feature_workspaces") },
  ];
  const lostFeatures = [
    t("workspace.settings.billing.hobby_confirm_feature_branding"),
    t("workspace.settings.billing.hobby_confirm_feature_contacts"),
    t("workspace.settings.billing.hobby_confirm_feature_ai"),
  ];
  const isBusy = isStartingTrial || isStartingHobby;

  return (
    <div className="flex w-full max-w-md flex-col items-center gap-y-6">
      <div className="relative w-full overflow-hidden rounded-xl border border-slate-200 bg-white shadow-lg">
        <div className="flex flex-col items-center gap-y-6 p-8">
          <div className="rounded-full bg-slate-100 p-4">
            <GiftIcon className="size-10 text-slate-600" />
          </div>
          <div className="text-center">
            <h1 className="text-2xl font-semibold text-slate-800">
              {t("workspace.settings.billing.select_plan_header", { count: trialDays })}
            </h1>
            <p className="mt-2 text-slate-600">{t("workspace.settings.billing.select_plan_subheader")}</p>
          </div>
          <ul className="my-3 w-full space-y-3 text-left">
            {features.map((feature) => (
              <li key={feature} className="flex items-center gap-3 text-slate-700">
                <CheckIcon className="size-5 shrink-0 text-slate-900" />
                <span>{feature}</span>
              </li>
            ))}
          </ul>
          <Button
            size="lg"
            onClick={() => void startTrial()}
            className="mt-4 w-full"
            loading={isStartingTrial}
            disabled={isBusy}>
            {t("workspace.settings.billing.select_plan_cta")}
          </Button>
        </div>
      </div>

      <button
        type="button"
        onClick={() => setShowHobbyConfirm(true)}
        disabled={isBusy}
        className="text-sm text-slate-400 underline-offset-2 transition-colors hover:text-slate-600 hover:underline">
        {t("workspace.settings.billing.select_plan_skip")}
      </button>

      <Dialog open={showHobbyConfirm} onOpenChange={setShowHobbyConfirm} width="narrow" hideCloseButton>
        <DialogHeader>
          <DialogTitle>{t("workspace.settings.billing.hobby_confirm_title")}</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <p className="text-slate-500">{t("workspace.settings.billing.hobby_confirm_description")}</p>
          <ul className="mt-4 space-y-1.5">
            {reducedLimits.map((item) => (
              <li key={item.to} className="flex items-center gap-2 text-sm text-slate-700">
                <XCircleIcon className="text-ink size-3.5 shrink-0" />
                <span>
                  <span className="text-slate-400 line-through">{item.from}</span>{" "}
                  <span aria-hidden="true">→</span> {item.to}
                </span>
              </li>
            ))}
            {lostFeatures.map((item) => (
              <li key={item} className="flex items-center gap-2 text-sm text-slate-700">
                <XCircleIcon className="text-ink size-3.5 shrink-0" />
                <span className="text-slate-400 line-through">{item}</span>
              </li>
            ))}
          </ul>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" loading={isStartingHobby} onClick={() => void continueOnHobby()}>
            {t("workspace.settings.billing.hobby_confirm_downgrade")}
          </Button>
          <Button
            onClick={() => {
              setShowHobbyConfirm(false);
              void startTrial();
            }}>
            {t("workspace.settings.billing.hobby_confirm_start_trial")}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
};
