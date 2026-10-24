import { createFileRoute, redirect } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { SetupLayout } from "../../components/shell/setup-layout";
import { authOptionsQuery } from "../../features/auth/api";
import { SignupForm } from "../../features/auth/components/signup-form";
import { homeQuery } from "../../lib/queries/organizations";

// forma2's fresh-instance sign-up, where the intro leads: the first account, framed as the instance's administrator. It is open while the instance has no user whatever SIGNUP_ENABLED says, because the server lets the first user in uninvited (apps/server/internal/auth/signup.go); once the instance has a user, or for a signed-in visitor, the root page decides instead, as the intro does.
export const Route = createFileRoute("/setup/signup")({
  beforeLoad: async ({ context }) => {
    const home = await context.queryClient.fetchQuery(homeQuery);
    if (home.path !== "/setup/intro") throw redirect({ to: "/", replace: true });
  },
  loader: ({ context }) => context.queryClient.ensureQueryData(authOptionsQuery),
  component: SetupSignupPage,
});

function SetupSignupPage() {
  const { t } = useTranslation();
  return (
    <SetupLayout>
      <div className="flex w-full flex-col items-center">
        <h2 className="mb-6 text-xl font-medium">{t("setup.signup.create_administrator")}</h2>
        <p className="text-sm text-slate-800">{t("setup.signup.this_user_has_all_the_power")}</p>
        <hr className="my-6 w-full border-slate-200" />
        <div className="w-full max-w-sm">
          <SignupForm search={{}} />
        </div>
      </div>
    </SetupLayout>
  );
}
