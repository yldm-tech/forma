import { responses } from "@/lib/api/response";
import { withV1ApiWrapper } from "@/lib/api/with-api-logging";
import { putResponseHandler } from "./lib/put-response-handler";

export const OPTIONS = async (): Promise<Response> => {
  return responses.successResponse({}, true);
};

export const PUT = withV1ApiWrapper({
  handler: putResponseHandler,
});
