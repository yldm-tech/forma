import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { Building2Icon, ChevronRightIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CreateWorkspaceModal } from "../../../../components/shell/create-workspace-modal";
import { OrganizationBreadcrumb } from "../../../../components/shell/organization-breadcrumb";
import { RoutePending } from "../../../../components/shell/route-states";
import { SwitcherDropdownBody } from "../../../../components/shell/switcher-dropdown-body";
import { UserDropdown } from "../../../../components/shell/user-dropdown";
import { Breadcrumb, BreadcrumbList } from "../../../../components/ui/breadcrumb";
import { Button } from "../../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { Wordmark } from "../../../../components/ui/logo";
import { sortByName } from "../../../../lib/navigation";
import { guardPageError } from "../../../../lib/route-guards";
import { meQuery } from "../../../../lib/queries/auth";
import {
  organizationNavigationQuery,
  organizationQuery,
  organizationsQuery,
} from "../../../../lib/queries/organizations";

// forma2's landing page, for a member who can open no workspace in the organization; anyone who can is sent to their first workspace instead.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/landing")({
  loader: async ({ context: { queryClient }, params, location }) => {
    const navigation = await queryClient
      .fetchQuery(organizationNavigationQuery(params.organizationId))
      .catch(guardPageError);
    if (navigation.path !== location.pathname) throw redirect({ href: navigation.path, replace: true });
    await queryClient.ensureQueryData(organizationQuery(params.organizationId)).catch(guardPageError);
    return navigation;
  },
  pendingComponent: RoutePending,
  component: Landing,
});

function Landing() {
  const { t } = useTranslation();
  const { organizationId } = Route.useParams();
  const navigation = Route.useLoaderData();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <div className="flex min-h-screen min-w-full flex-row">
      <LandingSidebar organizationId={organization.id} organizationName={organization.name} />
      <div className="flex flex-1 flex-col">
        <div className="flex items-center justify-between p-6">
          <Breadcrumb>
            <BreadcrumbList className="gap-0">
              <OrganizationBreadcrumb
                organizationId={organization.id}
                organizationName={organization.name}
                isLastCrumb
              />
            </BreadcrumbList>
          </Breadcrumb>
          <UserDropdown
            user={me}
            organizationId={organization.id}
            isBilling={organization.membership.role === "billing"}
          />
        </div>
        <main className="flex flex-1 flex-col items-center justify-center gap-y-12">
          <div className="text-center">
            <h1 className="text-3xl font-medium text-slate-800">
              {t("organizations.landing.no_workspaces_warning_title")}
            </h1>
            <p className="mt-2 text-sm text-slate-500">
              {t("organizations.landing.no_workspaces_warning_subtitle")}
            </p>
          </div>
          {navigation.canCreateWorkspace && (
            <Button type="button" onClick={() => setCreateOpen(true)}>
              <PlusIcon />
              {t("common.create_workspace")}
            </Button>
          )}
        </main>
      </div>
      <CreateWorkspaceModal open={createOpen} setOpen={setCreateOpen} organizationId={organization.id} />
    </div>
  );
}

const LandingSidebar = ({
  organizationId,
  organizationName,
}: {
  organizationId: string;
  organizationName: string;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const organizations = useQuery({ ...organizationsQuery, enabled: open });
  return (
    <aside className="w-sidebar-collapsed z-40 flex flex-col justify-between rounded-r-xl border-r border-slate-200 bg-white pt-3 shadow-md">
      <div className="px-3">
        <Wordmark className="text-2xl" />
      </div>
      <DropdownMenu onOpenChange={setOpen}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="flex w-full items-center gap-3 rounded-br-xl border-t px-3 py-3 text-left transition-colors duration-200 hover:bg-slate-50 focus-visible:ring-2 focus-visible:ring-slate-500 focus-visible:outline-hidden focus-visible:ring-inset">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-slate-100 text-slate-600">
              <Building2Icon className="size-4" strokeWidth={1.5} />
            </span>
            <span className="grow overflow-hidden">
              <span className="block truncate text-sm font-bold text-slate-700">{organizationName}</span>
              <span className="block text-sm text-slate-500">{t("common.organization")}</span>
            </span>
            <ChevronRightIcon className="size-4 shrink-0 text-slate-600" strokeWidth={1.5} />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="right" sideOffset={10} alignOffset={5} align="end">
          <SwitcherDropdownBody
            type="organization"
            isLoading={organizations.isLoading}
            isError={organizations.isError}
            onRetry={() => void organizations.refetch()}
            items={sortByName(organizations.data ?? [])}
            selectedId={organizationId}
            onSelect={(id) => void navigate({ href: `/organizations/${id}/` })}
            onSettings={() => void navigate({ href: `/organizations/${organizationId}/settings/general` })}
          />
        </DropdownMenuContent>
      </DropdownMenu>
    </aside>
  );
};
