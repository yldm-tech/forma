import { QueryClient } from "@tanstack/react-query";
import { describe, expect, test } from "vite-plus/test";
import { authKeys as shellAuthKeys } from "../../lib/queries/auth";
import { homeQuery, organizationKeys } from "../../lib/queries/organizations";
import { authKeys, resetSession } from "./api";

describe("resetSession", () => {
  test("drops every query of the previous session and keeps the instance's sign-in options", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(authKeys.options, { emailAuthEnabled: true });
    queryClient.setQueryData(authKeys.session, { id: "a" });
    queryClient.setQueryData(shellAuthKeys.me, { id: "a" });
    queryClient.setQueryData(homeQuery.queryKey, { path: "/auth/login", workspaceIds: [] });
    queryClient.setQueryData(organizationKeys.all, [{ id: "org-a" }]);

    resetSession(queryClient);

    expect(queryClient.getQueryData(authKeys.options)).toEqual({ emailAuthEnabled: true });
    expect(queryClient.getQueryData(authKeys.session)).toBeUndefined();
    expect(queryClient.getQueryData(shellAuthKeys.me)).toBeUndefined();
    expect(queryClient.getQueryData(homeQuery.queryKey)).toBeUndefined();
    expect(queryClient.getQueryData(organizationKeys.all)).toBeUndefined();
  });
});
