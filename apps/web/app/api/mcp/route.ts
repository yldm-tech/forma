import { NextRequest } from "next/server";
import { problemPayloadTooLarge } from "@/app/api/v3/lib/response";
import { RequestBodyTooLargeError, readRequestBodyWithLimit } from "@/lib/api/request-body";
import { handleAuthenticatedMcpRequest } from "@/modules/mcp/auth";
import { mcpHandler } from "@/modules/mcp/server";

export const runtime = "nodejs";
export const fetchCache = "force-no-store";

function getRequestId(request: NextRequest): string {
  return request.headers.get("x-request-id") ?? crypto.randomUUID();
}

export async function POST(request: NextRequest): Promise<Response> {
  // `content-length` on its own is not a bound: a chunked or otherwise streamed body carries none, and a header check has to treat that as "allowed". `readRequestBodyWithLimit` still rejects an oversized advertised length without reading anything, and counts the bytes as they arrive when there is no length to check.
  let body: string;
  try {
    body = await readRequestBodyWithLimit(request);
  } catch (error) {
    if (error instanceof RequestBodyTooLargeError) {
      return problemPayloadTooLarge(getRequestId(request), error.message, request.nextUrl.pathname);
    }

    throw error;
  }

  // The body has been consumed, so the MCP handler gets a request carrying the bytes we already read rather than a drained stream.
  return await handleAuthenticatedMcpRequest(new NextRequest(request, { body }), mcpHandler);
}
