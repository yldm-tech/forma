import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  ActivityIcon,
  PencilLineIcon,
  ShoppingCartIcon,
  SmileIcon,
  SparklesIcon,
  SquareLibraryIcon,
  StarIcon,
  ThumbsUpIcon,
  UsersIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { capturePostHogClientEvent } from "../../lib/telemetry/posthog";
import { getAIUnavailableMessage, workspaceAIQuery } from "../ai/availability";
import { CUSTOM_TEMPLATE_ID } from "../surveys/api";
import { useCreateSurveyFromTemplate } from "../surveys/templates/use-create-survey";
import { onboardingPath } from "./onboarding";
import { OnboardingBackLink, OnboardingOptions } from "./onboarding-options";

/**
 * forma2's CreateFirstSurvey, the onboarding step that creates the workspace's first survey: written by AI, from one of the experience-management templates, or from scratch. Each opens the editor in its CX mode; the AI option is disabled, with the reason under it, where AI is unavailable.
 */
export const CreateFirstSurvey = ({
  organizationId,
  workspaceId,
  defaultLanguage,
}: {
  organizationId: string;
  workspaceId: string;
  defaultLanguage: string;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { create, isPending } = useCreateSurveyFromTemplate("cx");
  const ai = useQuery(workspaceAIQuery(workspaceId));
  const isAIAvailable = ai.data?.available ?? false;
  const trackPathSelected = (path: "scratch" | "template" | "ai") =>
    capturePostHogClientEvent("onboarding_survey_path_selected", {
      path,
      organization_id: organizationId,
      workspace_id: workspaceId,
    });
  return (
    <OnboardingOptions
      title={t("organizations.workspaces.new.survey.title")}
      options={[
        {
          title: t("workspace.surveys.ai_create.create_with_ai"),
          description: t("organizations.workspaces.new.survey.create_with_ai_description"),
          icon: SparklesIcon,
          disabled: !isAIAvailable,
          disabledDescription: ai.isSuccess ? getAIUnavailableMessage(ai.data.reason, t) : undefined,
          onSelect: () => {
            trackPathSelected("ai");
            void navigate({ href: onboardingPath(organizationId, "ai") });
          },
        },
        {
          title: t("organizations.workspaces.new.survey.use_template"),
          description: t("organizations.workspaces.new.survey.use_template_description"),
          icon: SquareLibraryIcon,
          onSelect: () => {
            trackPathSelected("template");
            void navigate({ href: onboardingPath(organizationId, "templates") });
          },
        },
        {
          title: t("organizations.workspaces.new.survey.start_from_scratch"),
          description: t("organizations.workspaces.new.survey.start_from_scratch_description"),
          icon: PencilLineIcon,
          isLoading: isPending,
          onSelect: () => {
            trackPathSelected("scratch");
            void create({
              workspaceId,
              templateId: CUSTOM_TEMPLATE_ID,
              source: "custom",
              surveyType: "link",
              defaultLanguage,
            });
          },
        },
      ]}
    />
  );
};

/** forma2's XMTemplateList: the six experience-management templates, each creating a link survey and opening it in the editor's CX mode. */
export const XMTemplateList = ({
  organizationId,
  workspaceId,
  defaultLanguage,
}: {
  organizationId: string;
  workspaceId: string;
  defaultLanguage: string;
}) => {
  const { t } = useTranslation();
  const { create } = useCreateSurveyFromTemplate("cx");
  const [activeId, setActiveId] = useState<string | null>(null);

  const select = (templateId: string) => {
    setActiveId(templateId);
    void create({ workspaceId, templateId, source: "xm", surveyType: "link", defaultLanguage }).finally(() =>
      setActiveId(null)
    );
  };

  // forma2's XM_TEMPLATE_IDS, in its order.
  const cards = [
    {
      id: "nps",
      title: t("workspace.xm-templates.nps"),
      description: t("workspace.xm-templates.nps_description"),
      icon: ShoppingCartIcon,
    },
    {
      id: "star-rating",
      title: t("workspace.xm-templates.five_star_rating"),
      description: t("workspace.xm-templates.five_star_rating_description"),
      icon: StarIcon,
    },
    {
      id: "csat",
      title: t("workspace.xm-templates.csat"),
      description: t("workspace.xm-templates.csat_description"),
      icon: ThumbsUpIcon,
    },
    {
      id: "ces",
      title: t("workspace.xm-templates.ces"),
      description: t("workspace.xm-templates.ces_description"),
      icon: ActivityIcon,
    },
    {
      id: "smileys",
      title: t("workspace.xm-templates.smileys"),
      description: t("workspace.xm-templates.smileys_description"),
      icon: SmileIcon,
    },
    {
      id: "enps",
      title: t("workspace.xm-templates.enps"),
      description: t("workspace.xm-templates.enps_description"),
      icon: UsersIcon,
    },
  ];

  return (
    <OnboardingOptions
      title={t("workspace.xm-templates.headline")}
      back={<OnboardingBackLink organizationId={organizationId} />}
      options={cards.map((card) => ({
        title: card.title,
        description: card.description,
        icon: card.icon,
        isLoading: activeId === card.id,
        onSelect: () => select(card.id),
      }))}
    />
  );
};
