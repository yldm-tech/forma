import { createFileRoute, notFound } from "@tanstack/react-router";
import { authOptionsQuery } from "../../features/auth/api";
import { type SignupFormSearch, SignupForm } from "../../features/auth/components/signup-form";
import { redirectIfSignedIn, signupAvailable } from "../../features/auth/guards";
import { stringParam } from "../../features/auth/search";

export const Route = createFileRoute("/auth/signup")({
  validateSearch: (search: Record<string, unknown>): SignupFormSearch => ({
    inviteToken: stringParam(search.inviteToken),
    email: stringParam(search.email),
    error: stringParam(search.error),
  }),
  beforeLoad: redirectIfSignedIn,
  loaderDeps: ({ search }) => ({ inviteToken: search.inviteToken }),
  loader: async ({ context, deps }) => {
    const options = await context.queryClient.ensureQueryData(authOptionsQuery);
    if (!(await signupAvailable(options.signupEnabled, deps.inviteToken))) throw notFound();
  },
  component: SignupPage,
});

function SignupPage() {
  return <SignupForm search={Route.useSearch()} />;
}
