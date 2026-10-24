import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  AlertTriangleIcon,
  CheckIcon,
  ChevronDownIcon,
  CopyIcon,
  GlobeIcon,
  RotateCcwIcon,
  SmartphoneIcon,
  TagIcon,
  WrenchIcon,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { IdBadge } from "../../components/ui/id-badge";
import { LoadingBlock, PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { copyText } from "../../lib/clipboard";
import { cn } from "../../lib/cn";
import { errorMessage } from "../../lib/errors";
import { organizationSettingsPath } from "../../lib/navigation";
import { type AppConnection, type WorkspaceDetail, appConnectionQuery } from "../../lib/queries/workspaces";
import { sdkSnippets } from "../../lib/sdk-snippets";

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
          <ConnectionCards connection={connection.data} onRecheck={() => void connection.refetch()} />
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
          {connection.legacyEnvironmentId && (
            <IdBadge
              id={connection.legacyEnvironmentId}
              label={t("workspace.app-connection.environment_id_legacy")}
            />
          )}
          <IdBadge id={connection.appUrl} label={t("workspace.app-connection.webapp_url")} />
          {connection.legacyEnvironmentId && (
            <Alert variant="info" role="status">
              <AlertDescription>
                <p>{t("workspace.app-connection.environment_id_legacy_alert")}</p>
              </AlertDescription>
            </Alert>
          )}
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

const CodeBlock = ({ children }: { children: string }) => {
  const { t } = useTranslation();
  return (
    <div className="relative">
      <Button
        variant="ghost"
        size="icon"
        className="absolute top-2 right-2"
        aria-label={t("common.copy")}
        onClick={() => void copyText(children, t)}>
        <CopyIcon className="size-4" />
      </Button>
      <pre className="overflow-x-auto rounded-md border border-slate-200 bg-white p-4 pr-12 font-mono text-xs text-slate-800">
        <code>{children}</code>
      </pre>
    </div>
  );
};

interface InstallOption {
  id: string;
  label: string;
  steps: string[];
  snippet?: string;
}

const Steps = ({ option }: { option: InstallOption }) => {
  return (
    <div className="space-y-4">
      <ol className="list-inside list-decimal space-y-1 text-sm text-slate-700">
        {option.steps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
      {option.snippet && <CodeBlock>{option.snippet}</CodeBlock>}
    </div>
  );
};

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
      <Steps option={selected} />
    </div>
  );
};

/** forma2's InstallMethodCards, without the AI prompt forma2 showed only behind a PostHog experiment. */
const InstallMethods = ({ workspaceId, appUrl }: { workspaceId: string; appUrl: string }) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState<string | null>(null);
  const snippets = sdkSnippets(workspaceId, appUrl);
  const step = (key: string) => t(`workspace.app-connection.install_method_${key}`);
  const methods: { id: string; icon: ReactNode; title: string; description: string; body: ReactNode }[] = [
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
              steps: [step("html_step_1"), step("html_step_2")],
              snippet: snippets.html,
            },
            {
              id: "react",
              label: step("react"),
              steps: [step("react_step_1"), step("react_step_2")],
              snippet: snippets.react,
            },
            {
              id: "nextjs",
              label: step("nextjs"),
              steps: [step("nextjs_step_1"), step("nextjs_step_2"), step("nextjs_step_3")],
              snippet: snippets.nextjs,
            },
            {
              id: "vue",
              label: step("vue"),
              steps: [step("vue_step_1"), step("vue_step_2")],
              snippet: snippets.vue,
            },
          ]}
        />
      ),
    },
    {
      id: "mobile",
      icon: <SmartphoneIcon className="size-6" />,
      title: step("mobile"),
      description: step("mobile_description"),
      body: (
        <SubMethods
          options={[
            {
              id: "react-native",
              label: step("react_native"),
              steps: [step("react_native_step_1"), step("react_native_step_2")],
              snippet: snippets.reactNative,
            },
            {
              id: "swift",
              label: step("swift"),
              steps: [step("swift_step_1"), step("swift_step_2")],
              snippet: snippets.swift,
            },
            {
              id: "android",
              label: step("android"),
              steps: [step("android_step_1"), step("android_step_2")],
              snippet: snippets.android,
            },
            {
              id: "flutter",
              label: step("flutter"),
              steps: [step("flutter_step_1"), step("flutter_step_2")],
              snippet: snippets.flutter,
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
          option={{
            id: "gtm",
            label: step("gtm"),
            steps: [step("gtm_step_1"), step("gtm_step_2"), step("gtm_step_3"), step("gtm_step_4")],
            snippet: snippets.html,
          }}
        />
      ),
    },
    {
      id: "wordpress",
      icon: <WrenchIcon className="size-6" />,
      title: step("wordpress"),
      description: step("wordpress_description"),
      body: (
        <Steps
          option={{
            id: "wordpress",
            label: step("wordpress"),
            steps: [
              step("wordpress_step_1"),
              step("wordpress_step_2"),
              step("wordpress_step_3"),
              step("wordpress_step_4"),
            ],
          }}
        />
      ),
    },
  ];

  return (
    <div className="space-y-3">
      {methods.map((method) => {
        const isOpen = open === method.id;
        return (
          <div key={method.id} className="rounded-lg border border-slate-200 bg-white">
            <button
              type="button"
              aria-expanded={isOpen}
              onClick={() => setOpen(isOpen ? null : method.id)}
              className="flex w-full cursor-pointer items-center gap-4 p-5 text-left">
              <div className="flex size-12 shrink-0 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-slate-700">
                {method.icon}
              </div>
              <div className="grow">
                <p className="text-base font-semibold text-slate-800">{method.title}</p>
                <p className="text-sm text-slate-500">{method.description}</p>
              </div>
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
    </div>
  );
};
