import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../../lib/api";
import { organizationKeys } from "../../../lib/queries/organizations";
import { workspacesQuery } from "../../../lib/queries/workspaces";
import { WORKSPACE_ID_KEY, readStorage } from "../../../lib/storage";

const base = (organizationId: string) => `/api/app/organizations/${organizationId}`;

/** A survey with a pretty URL, forma2's TSurveyWithSlug. */
export interface PrettyUrlSurvey {
  id: string;
  name: string;
  slug: string;
  status: string;
  createdAt: string;
  workspace: { id: string; name: string; organizationId: string };
}

/** The surveys of the organization that have a pretty URL; self-hosted only. */
export const prettyUrlsQuery = (organizationId: string) =>
  queryOptions({
    queryKey: [...organizationKeys.detail(organizationId), "pretty-urls"],
    queryFn: ({ signal }) =>
      api
        .get<{ surveys: PrettyUrlSurvey[] }>(`${base(organizationId)}/pretty-urls`, { signal })
        .then((res) => res.surveys),
  });

/** Sets (a URL) or removes (null) one of the organization's whitelabel URLs, then reloads the organization. */
const useWhitelabelUrl = (organizationId: string, path: "email-logo" | "favicon", field: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (url: string | null) =>
      url === null
        ? api.delete<void>(`${base(organizationId)}/whitelabel/${path}`)
        : api.put<void>(`${base(organizationId)}/whitelabel/${path}`, { [field]: url }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organizationId) }),
  });
};

export const useEmailLogoUrl = (organizationId: string) =>
  useWhitelabelUrl(organizationId, "email-logo", "logoUrl");

export const useFaviconUrl = (organizationId: string) =>
  useWhitelabelUrl(organizationId, "favicon", "faviconUrl");

export const useSendTestEmail = (organizationId: string) =>
  useMutation({ mutationFn: () => api.post<void>(`${base(organizationId)}/whitelabel/test-email`) });

/** The workspace organization-level uploads are stored under, forma2's settings currentWorkspace: the one the user last opened when it belongs to the organization, otherwise its first. Empty while unknown or when there is none. */
export const useUploadWorkspaceId = (organizationId: string): string => {
  const { data: workspaces } = useQuery(workspacesQuery(organizationId));
  const remembered = readStorage(WORKSPACE_ID_KEY);
  if (!workspaces) return "";
  return workspaces.find((ws) => ws.id === remembered)?.id ?? workspaces[0]?.id ?? "";
};
