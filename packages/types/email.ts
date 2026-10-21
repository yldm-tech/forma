import { z } from "zod";
import { ZStorageUrl } from "./common";
import { ZUserLocale } from "./user";

export const ZLinkSurveyEmailData = z.object({
  surveyId: z.string(),
  // A single address, not a free string: the action behind this schema is unauthenticated and hands the
  // value to nodemailer, which parses a string `to` as an RFC address *list*. A plain `z.string()` therefore
  // turned one rate-limited request into a mail-blast to every comma-separated address in it. The email
  // format rejects commas, semicolons and angle brackets, so the list cannot be smuggled through.
  email: z.email(),
  suId: z.string().optional(),
  suToken: z.string().optional(),
  // The survey name is deliberately absent: the server already loads the row it would name, and taking it
  // from the caller let an unauthenticated request write the copy of a mail sent from the operator's domain.
  // The language the email's own copy is rendered in.
  locale: ZUserLocale,
  // The survey language the respondent arrived with, as stored on the survey (e.g. "de-DE"). Carried
  // back into the emailed link as `?lang=` so passing the gate does not drop the requested language.
  // Absent when the respondent made no explicit choice, so the link stays on the survey default.
  surveyLanguageCode: z.string().optional(),
  logoUrl: ZStorageUrl.optional(),
});

export type TLinkSurveyEmailData = z.infer<typeof ZLinkSurveyEmailData>;
