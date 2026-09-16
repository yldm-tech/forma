import type { McpServer } from "@modelcontextprotocol/server";
import { listV3Workspaces } from "@/app/api/v3/workspaces/lib/operations";
import { MCP_API_ROUTE } from "@/modules/mcp/constants";
import { getMcpAuthentication, getMcpRequestId, getMcpToolAuthInfo } from "../auth";
import { responseToMcpToolResult } from "../errors";
import { registerScopedTool } from "./guard-scopes";
import { type TMcpListWorkspacesInput, ZMcpListWorkspacesInput } from "./schemas";

export function registerWorkspaceTools(server: McpServer): void {
  // list_workspaces is the workspaceId-discovery prerequisite for the survey, workflow AND
  // workspaceId. The result is derived from the caller's own memberships/key grants, so admitting any
  // read scope exposes nothing extra.
  registerScopedTool(
    server,
    "list_workspaces",
    {
      title: "List workspaces",
      description:
        "List the Forma workspaces the authenticated user can access. Use this to discover the workspaceId required by the survey and workflow tools.",
      inputSchema: ZMcpListWorkspacesInput,
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    { anyOf: ["surveys:read", "workflows:read"] },
    async (_input: TMcpListWorkspacesInput, ctx) => {
      const authInfo = getMcpToolAuthInfo(ctx);
      const requestId = getMcpRequestId(authInfo);
      const response = await listV3Workspaces({
        authentication: getMcpAuthentication(authInfo),
        requestId,
        instance: MCP_API_ROUTE,
      });

      return await responseToMcpToolResult(response, requestId);
    }
  );
}
