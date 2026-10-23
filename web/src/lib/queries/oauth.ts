import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

/** An OAuth client's public metadata. */
export interface OAuthClient {
  clientId: string;
  name: string;
  uri: string | null;
  logoUri: string | null;
}

/** An authorization an MCP client started, waiting for the user's consent. */
export interface AuthorizationRequest {
  requestId: string;
  client: OAuthClient;
  redirectUri: string;
  scopes: string[];
  resource: string;
  /** False when the user already approved every scope for this client, so the page continues without asking. */
  consentRequired: boolean;
}

/** Where the browser goes after an answer: back to the client, with a code or with access_denied. */
export interface ConsentResult {
  redirect: boolean;
  url: string;
}

/** A client the user approved, and for which scopes. */
export interface OAuthConsent {
  id: string;
  client: OAuthClient;
  scopes: string[];
  createdAt: string;
  updatedAt: string;
}

export const oauthKeys = {
  all: ["oauth"] as const,
  request: (requestId: string) => [...oauthKeys.all, "authorization-requests", requestId] as const,
  consents: () => [...oauthKeys.all, "consents"] as const,
};

export const authorizationRequestQuery = (requestId: string) =>
  queryOptions({
    queryKey: oauthKeys.request(requestId),
    queryFn: ({ signal }) =>
      api.get<AuthorizationRequest>(
        `/api/app/oauth/authorization-requests/${encodeURIComponent(requestId)}`,
        { signal }
      ),
    // A request is answered once; refetching it after the answer would only find it gone.
    retry: false,
    staleTime: Infinity,
  });

export const useAnswerAuthorizationRequest = () =>
  useMutation({
    mutationFn: ({ requestId, accept }: { requestId: string; accept: boolean }) =>
      api.post<ConsentResult>(
        `/api/app/oauth/authorization-requests/${encodeURIComponent(requestId)}/consent`,
        { accept }
      ),
  });

export const oauthConsentsQuery = queryOptions({
  queryKey: oauthKeys.consents(),
  queryFn: ({ signal }) => api.get<OAuthConsent[]>("/api/app/oauth/consents", { signal }),
});

export const useRevokeOAuthConsent = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (consentId: string) =>
      api.delete<undefined>(`/api/app/oauth/consents/${encodeURIComponent(consentId)}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: oauthKeys.consents() }),
  });
};
