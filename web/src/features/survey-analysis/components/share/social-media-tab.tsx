import { AlertCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";
import { FacebookIcon, LinkedinIcon, RedditIcon, ThreadsIcon, XIcon } from "./social-icons";

/** The share links of the social networks, each carrying ?source=<network> so responses can be traced to it. */
export const socialShareLinks = (surveyUrl: string, surveyTitle: string) => {
  const tracked = (platform: string) =>
    `${surveyUrl}${surveyUrl.includes("?") ? "&" : "?"}source=${platform}`;
  const title = encodeURIComponent(surveyTitle);
  return [
    {
      name: "LinkedIn",
      icon: <LinkedinIcon />,
      url: `https://www.linkedin.com/shareArticle?mini=true&url=${encodeURIComponent(tracked("linkedin"))}&title=${title}`,
    },
    {
      name: "Threads",
      icon: <ThreadsIcon />,
      url: `https://www.threads.net/intent/post?text=${title}%20${encodeURIComponent(tracked("threads"))}`,
    },
    {
      name: "Facebook",
      icon: <FacebookIcon />,
      url: `https://www.facebook.com/sharer/sharer.php?u=${encodeURIComponent(tracked("facebook"))}`,
    },
    {
      name: "Reddit",
      icon: <RedditIcon />,
      url: `https://www.reddit.com/submit?url=${encodeURIComponent(tracked("reddit"))}&title=${title}`,
    },
    {
      name: "X",
      icon: <XIcon />,
      url: `https://twitter.com/intent/tweet?text=${title}&url=${encodeURIComponent(tracked("x"))}`,
    },
  ];
};

/** forma2's SocialMediaTab: share buttons for the networks, with source tracking. */
export const SocialMediaTab = ({ surveyUrl, surveyTitle }: { surveyUrl: string; surveyTitle: string }) => {
  const { t } = useTranslation();
  return (
    <>
      <div className="grid grid-cols-1 gap-4">
        {socialShareLinks(surveyUrl, surveyTitle).map((platform) => (
          <Button
            key={platform.name}
            variant="outline"
            className="w-fit bg-white"
            onClick={() =>
              window.open(
                platform.url,
                "share-dialog",
                "width=1024,height=768,location=no,toolbar=no,status=no,menubar=no,scrollbars=yes,resizable=yes,noopener=yes,noreferrer=yes"
              )
            }>
            {platform.name}
            {platform.icon}
          </Button>
        ))}
      </div>
      <Alert role="status">
        <AlertCircleIcon />
        <AlertTitle>{t("workspace.surveys.share.social_media.source_tracking_enabled")}</AlertTitle>
        <AlertDescription>
          {t("workspace.surveys.share.social_media.source_tracking_enabled_alert_description")}
        </AlertDescription>
        <AlertButton
          onClick={() =>
            window.open(
              "https://forma.yldm.ai/docs/surveys/link-surveys/source-tracking",
              "_blank",
              "noopener,noreferrer"
            )
          }>
          {t("common.learn_more")}
        </AlertButton>
      </Alert>
    </>
  );
};
