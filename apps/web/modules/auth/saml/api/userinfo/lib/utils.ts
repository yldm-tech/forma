// Returns null rather than throwing a `Response`: Next.js has no catch for a thrown Response, so it escapes the route handler as an unhandled error and the caller gets a 500 with a stack trace instead of the 401 challenge. The route turns the null into the response.
export const extractAuthToken = (req: Request): string | null => {
  const authHeader = req.headers.get("authorization");
  const parts = (authHeader || "").split(" ");
  if (parts.length > 1) return parts[1];

  // check for query param
  const params = new URL(req.url).searchParams;
  const accessToken = params.get("access_token");
  if (accessToken) return accessToken;

  return null;
};
