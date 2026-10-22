import { createFileRoute, redirect } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { SetupLayout } from "../../components/shell/setup-layout";
import { ButtonLink } from "../../features/auth/components/ui";
import { homeQuery } from "../../lib/queries/organizations";
import { boldRuns } from "../../lib/rich-text";

// forma2's fresh-instance intro: the root page sends a signed-out browser here while the instance has no user, and it leads to the sign-up of the first administrator. Once the instance has a user, or for a signed-in visitor, the root page decides instead, as forma2's fresh-instance layout refused the page then.
export const Route = createFileRoute("/setup/intro")({
  beforeLoad: async ({ context }) => {
    const home = await context.queryClient.fetchQuery(homeQuery);
    if (home.path !== "/setup/intro") throw redirect({ to: "/", replace: true });
  },
  component: IntroPage,
});

function IntroPage() {
  const { t } = useTranslation();
  const paragraphs = [
    t("setup.intro.paragraph_1"),
    t("setup.intro.paragraph_2"),
    t("setup.intro.paragraph_3"),
  ];
  return (
    <SetupLayout>
      <div className="flex flex-col items-center">
        <h1 className="mb-6 text-xl font-medium">{t("setup.intro.welcome_to_forma")}</h1>
        <div className="mx-auto max-w-sm space-y-4 text-sm leading-6 text-slate-600">
          {paragraphs.map((paragraph) => (
            <p key={paragraph}>
              {boldRuns(paragraph).map((run, i) =>
                run.bold ? <strong key={i}>{run.text}</strong> : <span key={i}>{run.text}</span>
              )}
            </p>
          ))}
        </div>
        <ButtonLink to="/auth/signup" className="mt-6">
          {t("setup.intro.get_started")}
        </ButtonLink>
        <p className="pt-6 text-xs text-slate-400">{t("setup.intro.made_with_love_in_kiel")}</p>
      </div>
    </SetupLayout>
  );
}
