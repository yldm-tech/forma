import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  AlertTriangleIcon,
  CheckIcon,
  ChevronDownIcon,
  ExternalLinkIcon,
  GlobeIcon,
  RotateCcwIcon,
  SmartphoneIcon,
  TagIcon,
  WrenchIcon,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { IdBadge } from "../../components/ui/id-badge";
import { LoadingBlock, PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { cn } from "../../lib/cn";
import { errorMessage } from "../../lib/errors";
import { organizationSettingsPath } from "../../lib/navigation";
import { type AppConnection, type WorkspaceDetail, appConnectionQuery } from "../../lib/queries/workspaces";
import { sdkReferenceUrl, sdkSnippets } from "../../lib/sdk-snippets";
import { CodeBlock } from "./code-block";
import { SdkIdentityCard } from "./sdk-identity";

/** forma2's app connection page: whether the SDK has reached the workspace, the ids it is set up with, and how to install it. */
export const AppConnectionPage = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const connection = useQuery(appConnectionQuery(workspace.id));
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.web_and_mobile_sdk")} />
      <div className="space-y-4">
        {/* Asking for a developer only makes sense until the SDK has connected; while loading or on an error it is not known yet. */}
        {connection.data && !connection.data.appSetupCompleted && (
          <Alert variant="info" role="status" className="rounded-xl">
            <AlertTitle>{t("workspace.app-connection.invite_banner_title")}</AlertTitle>
            <AlertDescription>{t("workspace.app-connection.invite_banner_description")}</AlertDescription>
            <Button
              variant="secondary"
              size="sm"
              asChild
              className="col-start-2 row-span-2 row-start-1 self-center">
              <Link to={organizationSettingsPath(workspace.organizationId, "teams")}>
                {t("workspace.app-connection.invite_banner_button")}
              </Link>
            </Button>
          </Alert>
        )}
        {connection.isPending ? (
          <LoadingBlock />
        ) : connection.isError ? (
          <div className="space-y-2">
            <p role="alert" className="text-sm text-slate-600">
              {errorMessage(t, connection.error)}
            </p>
            <Button variant="secondary" size="sm" onClick={() => void connection.refetch()}>
              {t("common.try_again")}
            </Button>
          </div>
        ) : (
          <>
            <ConnectionCards connection={connection.data} onRecheck={() => void connection.refetch()} />
            <SdkIdentityCard workspace={workspace} />
          </>
        )}
      </div>
    </PageContentWrapper>
  );
};

const ConnectionCards = ({ connection, onRecheck }: { connection: AppConnection; onRecheck: () => void }) => {
  const { t } = useTranslation();
  return (
    <>
      <SettingsCard
        width="full"
        title={t("workspace.app-connection.app_connection")}
        description={t("workspace.app-connection.app_connection_description")}>
        <div className="space-y-4 pb-4">
          <WidgetStatusIndicator appSetupCompleted={connection.appSetupCompleted} onRecheck={onRecheck} />
          {connection.appSetupCompleted && (
            <Alert variant="warning" role="status">
              <AlertTitle>{t("workspace.app-connection.cache_update_delay_title")}</AlertTitle>
              <AlertDescription>
                {t("workspace.app-connection.cache_update_delay_description")}
              </AlertDescription>
            </Alert>
          )}
        </div>
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("workspace.app-connection.workspace_details")}
        description={t("workspace.app-connection.workspace_details_description")}>
        <div className="space-y-3 pb-4">
          <IdBadge id={connection.workspaceId} label={t("workspace.app-connection.workspace_id")} />
          <IdBadge id={connection.appUrl} label={t("workspace.app-connection.webapp_url")} />
          {/* There is no staging environment per workspace: a second workspace is the staging one, and Copy to... (survey/copy.go) moves a finished survey across. */}
          <Alert variant="info" role="note">
            <AlertTitle>{t("workspace.app-connection.staging_workspace_title")}</AlertTitle>
            <AlertDescription>{t("workspace.app-connection.staging_workspace_description")}</AlertDescription>
          </Alert>
        </div>
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("workspace.app-connection.how_to_setup")}
        description={t("workspace.app-connection.how_to_setup_description")}>
        <div className="pb-4">
          <InstallMethods workspaceId={connection.workspaceId} appUrl={connection.appUrl} />
        </div>
      </SettingsCard>
    </>
  );
};

/** forma2's WidgetStatusIndicator: running once an SDK has fetched the workspace's state, otherwise not connected with a way to check again. */
const WidgetStatusIndicator = ({
  appSetupCompleted,
  onRecheck,
}: {
  appSetupCompleted: boolean;
  onRecheck: () => void;
}) => {
  const { t } = useTranslation();
  const Icon = appSetupCompleted ? CheckIcon : AlertTriangleIcon;
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center space-y-2 rounded-lg border py-6 text-center",
        appSetupCompleted ? "border-emerald-200 bg-emerald-100" : "border-slate-200 bg-slate-100"
      )}>
      <div
        className={cn(
          "flex h-12 w-12 items-center justify-center rounded-full border bg-white p-2",
          appSetupCompleted ? "border-emerald-200 text-emerald-700" : "border-slate-200 text-slate-700"
        )}>
        <Icon />
      </div>
      <p className="text-md font-bold text-slate-800 md:text-xl">
        {appSetupCompleted
          ? t("workspace.app-connection.receiving_data")
          : t("workspace.app-connection.forma_sdk_not_connected")}
      </p>
      <p className="w-2/3 text-sm text-balance text-slate-600">
        {appSetupCompleted
          ? t("workspace.app-connection.forma_sdk_connected")
          : t("workspace.app-connection.forma_sdk_not_connected_description")}
      </p>
      {!appSetupCompleted && (
        <Button variant="secondary" size="sm" className="bg-white" onClick={onRecheck}>
          <RotateCcwIcon />
          {t("workspace.app-connection.recheck")}
        </Button>
      )}
    </div>
  );
};

interface InstallStep {
  text: string;
  snippet?: string;
}

interface InstallOption {
  id: string;
  label: string;
  steps: InstallStep[];
}

/** Numbered steps, each followed by the code it asks to paste. */
const Steps = ({ steps }: { steps: InstallStep[] }) => (
  <ol className="list-inside list-decimal space-y-3 text-sm text-slate-700">
    {steps.map((step) => (
      <li key={step.text} className="space-y-2">
        <span>{step.text}</span>
        {step.snippet && <CodeBlock>{step.snippet}</CodeBlock>}
      </li>
    ))}
  </ol>
);

/** One install method: either its own steps, or a choice of frameworks each with theirs. */
const SubMethods = ({ options }: { options: InstallOption[] }) => {
  const [selectedId, setSelectedId] = useState(options[0]?.id);
  const selected = options.find((option) => option.id === selectedId) ?? options[0];
  if (!selected) return null;
  return (
    <div className="space-y-4">
      <div role="radiogroup" className="flex w-full gap-1 rounded-md border border-slate-200 bg-slate-50 p-1">
        {options.map((option) => (
          <button
            type="button"
            role="radio"
            key={option.id}
            aria-checked={option.id === selected.id}
            onClick={() => setSelectedId(option.id)}
            className={cn(
              "flex-1 rounded-md px-2 py-1.5 text-sm text-slate-700",
              option.id === selected.id ? "bg-white font-medium shadow-sm" : "hover:bg-slate-100"
            )}>
            {option.label}
          </button>
        ))}
      </div>
      <Steps steps={selected.steps} />
    </div>
  );
};

interface InstallMethod {
  id: string;
  icon: ReactNode;
  title: string;
  description: string;
  /** Absent for a method that is not available yet: its card says so and does not open. */
  body?: ReactNode;
}

/** How to install the SDK. Every working method loads it with the same script tag from this instance; nothing is published on npm, for mobile or for WordPress yet, so those cards say "coming soon" and give no install command. */
const InstallMethods = ({ workspaceId, appUrl }: { workspaceId: string; appUrl: string }) => {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState<string | null>(null);
  const snippets = sdkSnippets(workspaceId, appUrl);
  const step = (key: string) => t(`workspace.app-connection.install_method_${key}`);
  const usageStep: InstallStep = {
    text: t("workspace.app-connection.loader_usage_step"),
    snippet: snippets.usage,
  };
  const methods: InstallMethod[] = [
    {
      id: "website",
      icon: <GlobeIcon className="size-6" />,
      title: step("website"),
      description: step("website_description"),
      body: (
        <SubMethods
          options={[
            {
              id: "html",
              label: step("html"),
              steps: [{ text: step("html_step_1"), snippet: snippets.html }, { text: step("html_step_2") }],
            },
            {
              id: "react",
              label: step("react"),
              steps: [
                { text: t("workspace.app-connection.loader_react_step"), snippet: snippets.html },
                usageStep,
              ],
            },
            {
              id: "nextjs",
              label: step("nextjs"),
              steps: [
                { text: t("workspace.app-connection.loader_nextjs_step_1"), snippet: snippets.nextjs },
                { text: t("workspace.app-connection.loader_nextjs_step_2") },
                usageStep,
              ],
            },
            {
              id: "vue",
              label: step("vue"),
              steps: [
                { text: t("workspace.app-connection.loader_vue_step"), snippet: snippets.html },
                usageStep,
              ],
            },
          ]}
        />
      ),
    },
    {
      id: "google-tag-manager",
      icon: <TagIcon className="size-6" />,
      title: step("gtm"),
      description: step("gtm_description"),
      body: (
        <Steps
          steps={[
            { text: step("gtm_step_1") },
            { text: step("gtm_step_2"), snippet: snippets.html },
            { text: step("gtm_step_3") },
            { text: step("gtm_step_4") },
          ]}
        />
      ),
    },
    {
      id: "mobile",
      icon: <SmartphoneIcon className="size-6" />,
      title: step("mobile"),
      description: step("mobile_description"),
    },
    {
      id: "wordpress",
      icon: <WrenchIcon className="size-6" />,
      title: step("wordpress"),
      description: t("workspace.app-connection.loader_wordpress_description"),
    },
  ];

  return (
    <div className="space-y-3">
      {methods.map((method) => {
        const header = (
          <>
            <div className="flex size-12 shrink-0 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-slate-700">
              {method.icon}
            </div>
            <div className="grow">
              <p className="text-base font-semibold text-slate-800">{method.title}</p>
              <p className="text-sm text-slate-500">{method.description}</p>
            </div>
          </>
        );
        if (!method.body) {
          return (
            <div
              key={method.id}
              className="flex items-center gap-4 rounded-lg border border-slate-200 bg-white p-5">
              {header}
              <Badge text={t("workspace.app-connection.coming_soon")} type="gray" className="shrink-0" />
            </div>
          );
        }
        const isOpen = open === method.id;
        return (
          <div key={method.id} className="rounded-lg border border-slate-200 bg-white">
            <button
              type="button"
              aria-expanded={isOpen}
              onClick={() => setOpen(isOpen ? null : method.id)}
              className="flex w-full cursor-pointer items-center gap-4 p-5 text-left">
              {header}
              <ChevronDownIcon
                className={cn(
                  "size-5 shrink-0 text-slate-400 transition-transform duration-200",
                  isOpen && "rotate-180"
                )}
              />
            </button>
            {isOpen && (
              <div className="space-y-4 border-t border-slate-100 px-5 pt-4 pb-5">{method.body}</div>
            )}
          </div>
        );
      })}
      <p className="text-sm text-slate-600">
        {t("workspace.app-connection.loader_docs_description")}{" "}
        <a
          href={sdkReferenceUrl(i18n.language)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 font-medium text-slate-800 underline underline-offset-2">
          {t("workspace.app-connection.loader_docs_link")}
          <ExternalLinkIcon className="size-3.5" aria-hidden="true" />
        </a>
      </p>
    </div>
  );
};
