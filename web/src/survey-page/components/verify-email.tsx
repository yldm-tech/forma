import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { ArrowLeft, MailIcon } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { Button } from "../../components/ui/button";
import { FormError } from "../../components/ui/form-error";
import { Input } from "../../components/ui/input";
import { Toaster } from "../../components/ui/toaster";
import { cn } from "../../lib/cn";
import { errorMessage } from "../../lib/errors";
import { getLocalizedValue } from "../../lib/survey/i18n";
import { replaceHeadlineRecall } from "../../lib/survey/recall";
import { toast } from "../../lib/toast";
import { sendLinkSurveyEmail } from "../lib/api";
import type { TVerifyEmailState } from "../lib/state";
import { useAppLocale } from "../lib/use-app-locale";
import { getElementsFromSurveyBlocks } from "../lib/utils";
import { StackedCardsContainer } from "./stacked-cards-container";

const ZEmail = z.email();

/**
 * forma2's VerifyEmail gate: asks for the respondent's address and mails them the survey link with a token proving it, or, for a verification link that does not verify, says so. "Just curious?" lists the questions.
 */
export const VerifyEmail = ({
  fishy,
  survey,
  languageCode,
  styling,
  locale,
  singleUseId,
  singleUseToken,
}: TVerifyEmailState) => {
  const { t } = useTranslation();
  const isLocaleReady = useAppLocale(locale);
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | undefined>();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showPreviewQuestions, setShowPreviewQuestions] = useState(false);
  const [emailSent, setEmailSent] = useState(false);

  const localSurvey = useMemo(() => replaceHeadlineRecall(survey as unknown as TSurvey, "default"), [survey]);
  const questions = useMemo(() => getElementsFromSurveyBlocks(localSurvey.blocks), [localSurvey.blocks]);
  const cardArrangement =
    localSurvey.styling?.cardArrangement?.linkSurveys ?? styling.cardArrangement?.linkSurveys ?? "straight";
  const isCardless = cardArrangement === "cardless";
  const linkSurveyCardWidth =
    localSurvey.styling?.linkSurveyCardWidth ?? styling.linkSurveyCardWidth ?? "default";

  const submitEmail = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const parsed = ZEmail.safeParse(email);
    if (!parsed.success) {
      setError(parsed.error.issues[0]?.message);
      return;
    }
    setError(undefined);
    setIsSubmitting(true);
    try {
      await sendLinkSurveyEmail(localSurvey.id, {
        email: email.toLowerCase(),
        suId: singleUseId,
        suToken: singleUseToken ?? undefined,
        locale,
        // The language the respondent is reading the survey in, so the emailed link comes back to it.
        surveyLanguageCode: languageCode,
      });
      setEmailSent(true);
    } catch (err) {
      toast.error(errorMessage(t, err));
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleGoBackClick = () => {
    setShowPreviewQuestions(false);
    setEmailSent(false);
  };

  // The gate is nothing but translated chrome, so it waits for its locale instead of painting in another first.
  if (!isLocaleReady) return null;

  if (fishy) {
    return (
      <div className="flex h-screen w-screen flex-col items-center justify-center bg-slate-50">
        <span className="size-24 rounded-full bg-slate-300 p-6 text-5xl">🤔</span>
        <p className="mt-8 text-4xl font-bold">{t("s.this_looks_fishy")}</p>
        <Button variant="ghost" className="mt-4" onClick={handleGoBackClick}>
          {t("s.please_try_again_with_the_original_link")}
        </Button>
      </div>
    );
  }

  return (
    <div
      className={cn(
        "flex h-full w-full flex-col items-center justify-center text-center",
        isCardless ? "px-4 py-12 sm:px-6" : "p-2"
      )}>
      <Toaster />
      <StackedCardsContainer cardArrangement={cardArrangement} linkSurveyCardWidth={linkSurveyCardWidth}>
        <form noValidate onSubmit={(event) => void submitEmail(event)}>
          {!emailSent && !showPreviewQuestions && (
            <div className="flex flex-col">
              <div className="mx-auto rounded-full border bg-slate-200 p-6">
                <MailIcon strokeWidth={1.5} className="mx-auto size-12 text-white" />
              </div>
              <p className="mt-8 text-2xl font-bold lg:text-4xl">{t("s.verify_email_before_submission")}</p>
              <p className="mt-4 text-sm text-slate-500 lg:text-base">
                {t("s.verify_email_before_submission_description")}
              </p>
              <div className="my-4 w-full space-y-4">
                <div className="flex gap-x-2">
                  <Input
                    value={email}
                    onChange={(event) => setEmail(event.target.value)}
                    type="email"
                    aria-label={t("s.verify_email")}
                    aria-invalid={Boolean(error)}
                    placeholder="engineering@acme.com"
                    className="h-10 bg-white"
                  />
                  <Button
                    type="submit"
                    size="tall"
                    loading={isSubmitting}
                    className="focus-visible:ring-2 focus-visible:ring-slate-900 focus-visible:ring-offset-2">
                    {t("s.verify_email_before_submission_button")}
                  </Button>
                </div>
                {error && <FormError className="mt-2">{error}</FormError>}
              </div>
              <Button
                type="button"
                variant="ghost"
                className="mt-6"
                onClick={() => setShowPreviewQuestions(true)}>
                {t("s.just_curious")} <span>{t("s.preview_survey_questions")}</span>
              </Button>
            </div>
          )}
        </form>
        {!emailSent && showPreviewQuestions && (
          <div>
            <p className="text-2xl font-bold">{t("s.question_preview")}</p>
            <div className="mt-4 flex max-h-[50vh] w-full flex-col overflow-y-auto rounded-lg border border-slate-200 bg-slate-50/20 p-4 text-slate-700">
              {questions.map((question, index) => (
                <p key={question.id} className="my-1 text-sm">{`${(index + 1).toString()}. ${getTextContent(
                  getLocalizedValue(question.headline, languageCode)
                )}`}</p>
              ))}
            </div>
            <Button variant="ghost" className="mt-6" onClick={() => setShowPreviewQuestions(false)}>
              {t("s.want_to_respond")} <span>{t("s.verify_email")}</span>
            </Button>
          </div>
        )}
        {emailSent && (
          <div>
            <h1 className="mt-8 text-2xl font-bold lg:text-4xl">{t("s.survey_sent_to", { email })}</h1>
            <p className="mt-4 text-center text-sm text-slate-500 lg:text-base">
              {t("s.check_inbox_or_spam")}
            </p>
            <Button variant="secondary" className="mt-6" size="sm" onClick={handleGoBackClick}>
              <ArrowLeft />
              {t("common.back")}
            </Button>
          </div>
        )}
      </StackedCardsContainer>
    </div>
  );
};
