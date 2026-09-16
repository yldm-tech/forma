import { TooManyRequestsError } from "@forma/types/errors";
import { reportApiError } from "@/lib/api/api-error-reporter";
import { responses } from "@/lib/api/response";

const rateLimitMessage = "Maximum number of requests reached. Please try again later.";
const unexpectedErrorMessage = "Something went wrong. Please try again.";

export const isTooManyRequestsError = (error: unknown): boolean => {
  return (
    error instanceof TooManyRequestsError || (error instanceof Error && error.name === "TooManyRequestsError")
  );
};

export const getRateLimitErrorResponse = ({
  request,
  error,
  cors = true,
}: {
  request: Request;
  error: unknown;
  cors?: boolean;
}): Response => {
  if (isTooManyRequestsError(error)) {
    return responses.tooManyRequestsResponse(rateLimitMessage, cors);
  }

  const response = responses.internalServerErrorResponse(unexpectedErrorMessage, cors);
  reportApiError({
    request,
    status: response.status,
    error,
  });
  return response;
};
