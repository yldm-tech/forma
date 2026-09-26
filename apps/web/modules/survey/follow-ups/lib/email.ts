import { TResponse } from "@forma/types/responses";
import { TSurveyFollowUp } from "@forma/types/surveys/follow-up";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { sendEmail } from "@/modules/email";
import { buildSurveyResponseEmailHtml } from "@/modules/email/lib/survey-response-email";

export const sendFollowUpEmail = async ({
  followUp,
  to,
  replyTo,
  survey,
  response,
  attachResponseData = false,
  includeVariables = false,
  includeHiddenFields = false,
  logoUrl,
  locale,
}: {
  followUp: TSurveyFollowUp;
  to: string;
  replyTo: string[];
  attachResponseData: boolean;
  includeVariables?: boolean;
  includeHiddenFields?: boolean;
  survey: TSurvey;
  response: TResponse;
  logoUrl?: string;
  locale?: TUserLocale;
}): Promise<boolean> => {
  const {
    action: {
      properties: { subject, body },
    },
  } = followUp;

  const emailHtmlBody = await buildSurveyResponseEmailHtml({
    body,
    survey,
    response,
    attachResponseData,
    includeVariables,
    includeHiddenFields,
    logoUrl,
    locale,
  });

  // Returned rather than discarded: sendEmail does not throw when SMTP is unconfigured, it returns false. A
  // caller that ignores that reports a follow-up as sent when nothing left the box.
  return await sendEmail({
    to,
    replyTo: replyTo.join(", "),
    subject,
    html: emailHtmlBody,
  });
};
