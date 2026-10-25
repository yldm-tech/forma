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
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FormError } from "../../components/ui/form-error";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { errorMessage } from "../../lib/errors";
import { useUpdateWorkspace } from "../../lib/queries/workspaces";
import { capturePostHogClientEvent } from "../../lib/telemetry/posthog";
import { getAIUnavailableMessage, workspaceAIQuery } from "../ai/availability";
import { CUSTOM_TEMPLATE_ID } from "../surveys/api";
import { useCreateSurveyFromTemplate } from "../surveys/templates/use-create-survey";
import { onboardingPath } from "./onboarding";
import { OnboardingBackLink, OnboardingOptions } from "./onboarding-options";
import { initialProductName } from "./product-name";

type SurveyPath = "scratch" | "template" | "ai";

/**
 * forma2's CreateFirstSurvey, the onboarding step that creates the workspace's first survey: written by AI, from one of the experience-management templates, or from scratch. Each opens the editor in its CX mode; the AI option is disabled, with the reason under it, where AI is unavailable.
 *
 * Above the options it asks what the product or company is called. The templates write the workspace's name into their questions when a survey is created, so whichever option is chosen, a name typed here becomes the workspace's name first; left empty, the workspace keeps its name.
 */
export const CreateFirstSurvey = ({
  organizationId,
  workspaceId,
  workspaceName,
  email,
  defaultLanguage,
}: {
  organizationId: string;
  workspaceId: string;
  workspaceName: string;
  /** The signed-in user's address, whose domain suggests the name when the workspace still has the default one. */
  email: string;
  defaultLanguage: string;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { create, isPending } = useCreateSurveyFromTemplate("cx");
  const ai = useQuery(workspaceAIQuery(workspaceId));
  const isAIAvailable = ai.data?.available ?? false;
  const updateWorkspace = useUpdateWorkspace(workspaceId);
  const [productName, setProductName] = useState(() => initialProductName(workspaceName, email));
  const [productNameError, setProductNameError] = useState<string>();
  const [selected, setSelected] = useState<SurveyPath>();
  const productNameRef = useRef<HTMLInputElement>(null);
  // Ignores a second click before the first one's state has rendered.
  const choosingRef = useRef(false);

  /** Names the workspace after the product when the field holds a new name; false, with the reason under the field, when the server refused it. */
  const saveProductName = async (): Promise<boolean> => {
    const name = productName.trim();
    if (name === "" || name === workspaceName) return true;
    try {
      await updateWorkspace.mutateAsync({ name });
      return true;
    } catch (error) {
      setProductNameError(errorMessage(t, error));
      productNameRef.current?.focus();
      return false;
    }
  };

  const choose = async (path: SurveyPath, next: () => Promise<unknown>) => {
    if (choosingRef.current) return;
    choosingRef.current = true;
    setSelected(path);
    setProductNameError(undefined);
    capturePostHogClientEvent("onboarding_survey_path_selected", {
      path,
      organization_id: organizationId,
      workspace_id: workspaceId,
    });
    try {
      if (!(await saveProductName())) return;
      await next();
    } finally {
      choosingRef.current = false;
      setSelected(undefined);
    }
  };

  return (
    <OnboardingOptions
      title={t("organizations.workspaces.new.survey.title")}
      intro={
        <div className="flex w-full max-w-sm flex-col gap-y-2 text-left">
          <Label htmlFor="onboarding-product-name">{t("onboarding.product_name_label")}</Label>
          <Input
            ref={productNameRef}
            id="onboarding-product-name"
            value={productName}
            onChange={(event) => {
              setProductName(event.target.value);
              setProductNameError(undefined);
            }}
            placeholder={t("onboarding.product_name_placeholder")}
            autoComplete="organization"
            isInvalid={productNameError !== undefined}
            aria-describedby={
              productNameError ? "onboarding-product-name-error" : "onboarding-product-name-description"
            }
          />
          <p id="onboarding-product-name-description" className="text-xs text-slate-500">
            {t("onboarding.product_name_description")}
          </p>
          <FormError id="onboarding-product-name-error">{productNameError}</FormError>
        </div>
      }
      options={[
        {
          title: t("workspace.surveys.ai_create.create_with_ai"),
          description: t("organizations.workspaces.new.survey.create_with_ai_description"),
          icon: SparklesIcon,
          disabled: !isAIAvailable,
          disabledDescription: ai.isSuccess ? getAIUnavailableMessage(ai.data.reason, t) : undefined,
          isLoading: selected === "ai",
          onSelect: () => void choose("ai", () => navigate({ href: onboardingPath(organizationId, "ai") })),
        },
        {
          title: t("organizations.workspaces.new.survey.use_template"),
          description: t("organizations.workspaces.new.survey.use_template_description"),
          icon: SquareLibraryIcon,
          isLoading: selected === "template",
          onSelect: () =>
            void choose("template", () => navigate({ href: onboardingPath(organizationId, "templates") })),
        },
        {
          title: t("organizations.workspaces.new.survey.start_from_scratch"),
          description: t("organizations.workspaces.new.survey.start_from_scratch_description"),
          icon: PencilLineIcon,
          isLoading: selected === "scratch" || isPending,
          onSelect: () =>
            void choose("scratch", () =>
              create({
                workspaceId,
                templateId: CUSTOM_TEMPLATE_ID,
                source: "custom",
                surveyType: "link",
                defaultLanguage,
              })
            ),
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
