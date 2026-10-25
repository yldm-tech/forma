import { queryOptions, useMutation, useQuery } from "@tanstack/react-query";
import DOMPurify from "isomorphic-dompurify";
import { CopyIcon, SendIcon } from "lucide-react";
import { type SyntheticEvent, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { LoadingSpinner } from "../../../../components/ui/loading-spinner";
import { TabBar } from "../../../../components/ui/tab-bar";
import { api } from "../../../../lib/api";
import { copyText } from "../../../../lib/clipboard";
import { errorMessage } from "../../../../lib/errors";
import { authOptionsQuery } from "../../../../lib/queries/auth";
import { toast } from "../../../../lib/toast";
import { CodeBlock } from "./code-block";

/** GET /api/app/surveys/{id}/email-html: forma2's getEmailHtmlAction, the survey's email embed with its links marked as previews. */
export const emailHtmlQuery = (surveyId: string) =>
  queryOptions({
    queryKey: ["surveys", surveyId, "email-html"] as const,
    queryFn: ({ signal }) =>
      api
        .get<{ html: string }>(`/api/app/surveys/${surveyId}/email-html`, { signal })
        .then((res) => res.html),
  });

/** What leaves the app, in the copied code and the mailed preview: the survey's links without their preview mark. */
export const stripPreviewParam = (html: string): string =>
  html
    .replaceAll("?preview=true&amp;", "?")
    .replaceAll("?preview=true&;", "?")
    .replaceAll("?preview=true", "");

/** The sanitized embed as a light-mode document for the preview frame. */
const previewDocument = (sanitized: string): string => `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="color-scheme" content="only light" />
    <meta name="supported-color-schemes" content="light" />
    <base target="_blank" />
    <style>
      :root { color-scheme: only light; supported-color-schemes: light; }
      html, body { margin: 0; padding: 0; background: #ffffff; color-scheme: only light; }
    </style>
  </head>
  <body>${sanitized}</body>
</html>`;

const MIN_FRAME_HEIGHT = 560;

/**
 * forma2's EmailTab: the survey's first question as an email, previewed as the recipient sees it or as the code to paste into an email; the member can mail the preview to themselves.
 */
export const EmailTab = ({ surveyId, email }: { surveyId: string; email: string }) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState("preview");
  const [frameHeight, setFrameHeight] = useState(MIN_FRAME_HEIGHT);
  const html = useQuery(emailHtmlQuery(surveyId));
  const { data: options } = useQuery(authOptionsQuery);
  // An instance without SMTP answers every preview with sent: false, so the button says why up front instead of failing with a generic error.
  const emailUnavailable = options?.smtpConfigured === false;
  const send = useMutation({
    mutationFn: () => api.post<{ sent: boolean }>(`/api/app/surveys/${surveyId}/email-preview`),
    onSuccess: ({ sent }) => {
      if (sent) toast.success(t("workspace.surveys.share.send_email.email_sent"));
      else toast.error(t("common.something_went_wrong_please_try_again"));
    },
    onError: (error) => toast.error(errorMessage(t, error)),
  });

  const embedCode = useMemo(() => stripPreviewParam(html.data ?? ""), [html.data]);
  const previewDoc = useMemo(
    () =>
      html.data ? previewDocument(DOMPurify.sanitize(html.data, { ADD_ATTR: ["bgcolor", "target"] })) : "",
    [html.data]
  );

  const handleFrameLoad = (event: SyntheticEvent<HTMLIFrameElement>) => {
    const doc = event.currentTarget.contentDocument;
    if (!doc) return;
    setFrameHeight(Math.max(doc.body.scrollHeight, doc.documentElement.scrollHeight, MIN_FRAME_HEIGHT));
  };

  const tabs = [
    { id: "preview", label: t("workspace.surveys.share.send_email.email_preview_tab") },
    { id: "embed", label: t("workspace.surveys.share.send_email.embed_code_tab") },
  ];

  return (
    <div className="flex h-full w-full flex-col gap-y-4">
      <TabBar
        tabs={tabs}
        activeId={activeTab}
        setActiveId={setActiveTab}
        tabStyle="button"
        className="h-10 min-h-10 rounded-md border border-slate-200 bg-slate-100"
      />
      <div className="flex-1">
        {activeTab === "preview" ? (
          <div className="space-y-4 pb-4">
            <div
              className="flex-1 overflow-y-auto rounded-lg border border-slate-200 bg-white p-4"
              data-testid="survey-email-preview-shell">
              <div className="mb-6 flex gap-2">
                <div className="size-3 rounded-full bg-red-500" />
                <div className="size-3 rounded-full bg-amber-500" />
                <div className="size-3 rounded-full bg-emerald-500" />
              </div>
              <div className="mb-2 border-b border-slate-200 pb-2 text-sm">
                {t("workspace.surveys.share.send_email.email_to_label")} : {email || "user@mail.com"}
              </div>
              <div className="border-b border-slate-200 pb-2 text-sm">
                {t("workspace.surveys.share.send_email.email_subject_label")} :{" "}
                {t("workspace.surveys.share.send_email.forma_email_survey_preview")}
              </div>
              {html.isError ? (
                <p className="mt-2 text-sm text-slate-600">{errorMessage(t, html.error)}</p>
              ) : previewDoc ? (
                <iframe
                  className="mt-2 w-full rounded-md border-0 bg-white"
                  data-testid="survey-email-preview-frame"
                  onLoad={handleFrameLoad}
                  sandbox="allow-popups allow-popups-to-escape-sandbox allow-same-origin"
                  srcDoc={previewDoc}
                  style={{ height: `${frameHeight}px` }}
                  title={t("workspace.surveys.share.send_email.email_preview_tab")}
                />
              ) : (
                <LoadingSpinner />
              )}
            </div>
            <Button
              title={t("workspace.surveys.share.send_email.send_preview_email")}
              aria-label={t("workspace.surveys.share.send_email.send_preview_email")}
              aria-describedby={emailUnavailable ? "email-preview-unavailable" : undefined}
              disabled={emailUnavailable}
              onClick={() => send.mutate()}
              loading={send.isPending}
              className="shrink-0">
              {t("workspace.surveys.share.send_email.send_preview")}
              <SendIcon />
            </Button>
            {emailUnavailable && (
              <p id="email-preview-unavailable" className="max-w-sm text-sm text-slate-600">
                {t("workspace.settings.general.test_email_requires_smtp")}
              </p>
            )}
          </div>
        ) : (
          <div className="space-y-4 pb-4">
            <CodeBlock className="h-96 overflow-y-scroll text-sm">{embedCode}</CodeBlock>
            <Button
              title={t("workspace.surveys.share.send_email.copy_embed_code")}
              aria-label={t("workspace.surveys.share.send_email.copy_embed_code")}
              disabled={!html.data}
              onClick={() =>
                void copyText(
                  embedCode,
                  t,
                  t("workspace.surveys.share.send_email.embed_code_copied_to_clipboard")
                )
              }
              className="shrink-0">
              {t("common.copy_code")}
              <CopyIcon />
            </Button>
          </div>
        )}
      </div>
    </div>
  );
};
