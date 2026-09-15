"use server";

import Link from "next/link";
import { WidgetStatusIndicator } from "@/app/(app)/workspaces/[workspaceId]/components/WidgetStatusIndicator";
import { SettingsCard } from "@/app/(app)/workspaces/[workspaceId]/settings/components/SettingsCard";
import { WEBAPP_URL } from "@/lib/constants";
import { getPostHogFeatureFlag } from "@/lib/posthog/get-feature-flag";
import { getTranslate } from "@/lingodotdev/server";
import { organizationSettingsPath } from "@/modules/settings/lib/routes";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "@/modules/ui/components/alert";
import { IdBadge } from "@/modules/ui/components/id-badge";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { getWorkspaceAuth } from "@/modules/workspaces/lib/utils";
import { InstallMethodCards } from "./components/install-method-cards";

export const AppConnectionPage = async ({ params }: { params: Promise<{ workspaceId: string }> }) => {
  const t = await getTranslate();
  const { workspaceId } = await params;
  const workspaceIdMigrationUrl =
    "https://forma.ylam.ai/docs/surveys/website-app-surveys/workspace-id-migration";

  const { workspace, organization, session } = await getWorkspaceAuth(workspaceId);
  const showAIPrompt = session?.user.id
    ? (await getPostHogFeatureFlag(session.user.id, "a-b_app-connection_ai-prompt", { workspaceId })) ===
      "test"
    : false;

  const aiPrompt = `Integrate Forma into my app. 
  
Detect my framework from the project files and follow the matching instructions below.

Workspace ID : ${workspace.id}
App URL      : ${WEBAPP_URL}

---

## HTML (no framework)
Paste this snippet into your <head> on every page:

  <!-- START Forma Surveys -->
  <script type="text/javascript">
  !function(){
      var appUrl = "${WEBAPP_URL}";
      var workspaceId = "${workspace.id}";
  var t=document.createElement("script");t.type="text/javascript",t.async=!0,t.src=appUrl+"/js/forma.umd.cjs";var e=document.getElementsByTagName("script")[0];e.parentNode.insertBefore(t,e),setTimeout(function(){window.forma.setup({workspaceId: workspaceId, appUrl: appUrl})},500)}();
  </script>
  <!-- END Forma Surveys -->

## React.js
1. Install: npm install @formbricks/js zod
2. In src/App.js (or App.tsx), add at the top level:

  import forma from "@formbricks/js";
  if (typeof window !== "undefined") {
    forma.setup({ workspaceId: "${workspace.id}", appUrl: "${WEBAPP_URL}" });
  }

## Next.js — App Router
1. Install: npm install @formbricks/js zod
2. Create app/forma.tsx:

  "use client";
  import { usePathname, useSearchParams } from "next/navigation";
  import { useEffect } from "react";
  import forma from "@formbricks/js";

  export default function FormaProvider() {
    const pathname = usePathname();
    const searchParams = useSearchParams();
    useEffect(() => {
      forma.setup({ workspaceId: "${workspace.id}", appUrl: "${WEBAPP_URL}" });
    }, []);
    useEffect(() => { forma?.registerRouteChange(); }, [pathname, searchParams]);
    return null;
  }

3. In app/layout.tsx, add inside <html>:

  import { Suspense } from "react";
  import FormaProvider from "./forma";
  // ...
  <Suspense><FormaProvider /></Suspense>

## Next.js — Pages Router
1. Install: npm install @formbricks/js zod
2. In src/pages/_app.tsx:

  import { useRouter } from "next/router";
  import { useEffect } from "react";
  import forma from "@formbricks/js";

  if (typeof window !== "undefined") {
    forma.setup({ workspaceId: "${workspace.id}", appUrl: "${WEBAPP_URL}" });
  }
  export default function App({ Component, pageProps }) {
    const router = useRouter();
    useEffect(() => {
      const handleRouteChange = forma?.registerRouteChange;
      router.events.on("routeChangeComplete", handleRouteChange);
      return () => router.events.off("routeChangeComplete", handleRouteChange);
    }, []);
    return <Component {...pageProps} />;
  }

## Vue.js
1. Install: npm install @formbricks/js
2. Create src/forma.js:

  import forma from "@formbricks/js";
  if (typeof window !== "undefined") {
    forma.setup({ workspaceId: "${workspace.id}", appUrl: "${WEBAPP_URL}" });
  }
  export default forma;

3. In src/main.js, import forma and add:

  router.afterEach(() => {
    if (typeof forma !== "undefined") forma.registerRouteChange();
  });

## React Native
1. Install: npm install @forma/react-native
2. In App.js/App.tsx:

  import Forma from "@forma/react-native";
  const config = { workspaceId: "${workspace.id}", appUrl: "${WEBAPP_URL}" };
  // Render <Forma initConfig={config} /> inside your root component.

## Flutter
1. Add to pubspec.yaml: forma (run flutter pub add forma)
2. Mount the widget high in your widget tree:

  Forma(appUrl: "${WEBAPP_URL}", workspaceId: "${workspace.id}")

3. Drive it via static API: await Forma.track("event"), Forma.setUserId("uid"), etc.

## iOS (Swift)
1. Add via Swift Package Manager: https://github.com/yldm-tech/ios.git
2. Initialize on app launch:

  import FormaSDK
  let config = FormaConfig.Builder(appUrl: "${WEBAPP_URL}", workspaceId: "${workspace.id}").build()
  Forma.setup(with: config)
  Forma.setUserId("your-user-id")

## Android (Kotlin)
1. Add to build.gradle.kts:
   implementation("com.forma:android:2.1.0")
   Also enable dataBinding = true under android.buildFeatures.
2. Initialize in your Activity:

  val config = FormaConfig.Builder("${WEBAPP_URL}", "${workspace.id}")
    .setFragmentManager(supportFragmentManager).build()
  Forma.setup(this, config)
  Forma.setUserId("your-user-id")

---

## After setup — identify users

Call setUserId with the authenticated user's ID. Call logout() on sign-out.

  // Web
  forma.setUserId("your-user-id");
  forma.logout();

## Validate

Go to Settings → ${t("common.web_and_mobile_sdk")}. The widget indicator should turn green.
To debug, add ?formaDebug=true to your app URL and check the browser console.`;

  const htmlSnippet = `<!-- START Forma Surveys -->
<script type="text/javascript">
!function(){
    var appUrl = "${WEBAPP_URL}";
    var workspaceId = "${workspace.id}";
var t=document.createElement("script");t.type="text/javascript",t.async=!0,t.src=appUrl+"/js/forma.umd.cjs";var e=document.getElementsByTagName("script")[0];e.parentNode.insertBefore(t,e),setTimeout(function(){window.forma.setup({workspaceId: workspaceId, appUrl: appUrl})},500)}();
</script>
<!-- END Forma Surveys -->`;

  const nextjsSnippet = `"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { useEffect } from "react";
import forma from "@formbricks/js";

export default function FormaProvider() {
  const pathname = usePathname();
  const searchParams = useSearchParams();

  useEffect(() => {
    forma.setup({ workspaceId: "${workspace.id}", appUrl: "${WEBAPP_URL}" });
  }, []);

  useEffect(() => {
    forma?.registerRouteChange();
  }, [pathname, searchParams]);

  return null;
}`;

  const reactSnippet = `import forma from "@formbricks/js";

if (typeof window !== "undefined") {
  forma.setup({
    workspaceId: "${workspace.id}",
    appUrl: "${WEBAPP_URL}",
  });
}`;

  const vueSnippet = `import forma from "@formbricks/js";

if (typeof window !== "undefined") {
  forma.setup({
    workspaceId: "${workspace.id}",
    appUrl: "${WEBAPP_URL}",
  });
}

export default forma;`;

  const reactNativeSnippet = `import Forma from "@forma/react-native";

const config = {
  workspaceId: "${workspace.id}",
  appUrl: "${WEBAPP_URL}",
};

export default function App() {
  return (
    <>
      {/* Your app content */}
      <Forma initConfig={config} />
    </>
  );
}`;

  const androidSnippet = `val config = FormaConfig.Builder(
    "${WEBAPP_URL}",
    "${workspace.id}"
)
  .setFragmentManager(supportFragmentManager)
  .build()

Forma.setup(this, config)`;

  const swiftSnippet = `import FormaSDK

let config = FormaConfig.Builder(
    appUrl: "${WEBAPP_URL}",
    workspaceId: "${workspace.id}"
).build()

Forma.setup(with: config)`;

  const flutterSnippet = `import 'package:forma/forma.dart';

Forma(
  appUrl: "${WEBAPP_URL}",
  workspaceId: "${workspace.id}",
);`;

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.web_and_mobile_sdk")} />
      <div className="space-y-4">
        <Alert variant="info" role="status" className="max-w-4xl rounded-xl">
          <AlertTitle>{t("workspace.app-connection.invite_banner_title")}</AlertTitle>
          <AlertDescription>{t("workspace.app-connection.invite_banner_description")}</AlertDescription>
          <AlertButton asChild>
            <Link href={organizationSettingsPath(organization.id, "teams")}>
              {t("workspace.app-connection.invite_banner_button")}
            </Link>
          </AlertButton>
        </Alert>
        <SettingsCard
          title={t("workspace.app-connection.app_connection")}
          description={t("workspace.app-connection.app_connection_description")}>
          {workspace && (
            <div className="space-y-4">
              <WidgetStatusIndicator workspace={workspace} />
              {workspace.appSetupCompleted && (
                <Alert variant="warning" role="status">
                  <AlertTitle>{t("workspace.app-connection.cache_update_delay_title")}</AlertTitle>
                  <AlertDescription>
                    {t("workspace.app-connection.cache_update_delay_description")}
                  </AlertDescription>
                </Alert>
              )}
            </div>
          )}
        </SettingsCard>
        <SettingsCard
          title={t("workspace.app-connection.workspace_details")}
          description={t("workspace.app-connection.workspace_details_description")}>
          <div className="space-y-3">
            <IdBadge id={workspace.id} label={t("workspace.app-connection.workspace_id")} />
            {workspace.legacyEnvironmentId && (
              <IdBadge
                id={workspace.legacyEnvironmentId}
                label={t("workspace.app-connection.environment_id_legacy")}
              />
            )}
            <IdBadge id={WEBAPP_URL} label={t("workspace.app-connection.webapp_url")} />
            {workspace.legacyEnvironmentId && (
              <Alert variant="info" size="small" role="status">
                <AlertDescription>
                  <p>
                    {t("workspace.app-connection.environment_id_legacy_alert")}{" "}
                    <Link href={workspaceIdMigrationUrl} target="_blank" rel="noopener noreferrer">
                      {t("workspace.app-connection.environment_id_legacy_alert_link")}
                    </Link>
                  </p>
                </AlertDescription>
              </Alert>
            )}
          </div>
        </SettingsCard>
        <SettingsCard
          title={t("workspace.app-connection.how_to_setup")}
          description={t("workspace.app-connection.how_to_setup_description")}>
          <InstallMethodCards
            htmlSnippet={htmlSnippet}
            reactSnippet={reactSnippet}
            nextjsSnippet={nextjsSnippet}
            vueSnippet={vueSnippet}
            reactNativeSnippet={reactNativeSnippet}
            swiftSnippet={swiftSnippet}
            androidSnippet={androidSnippet}
            flutterSnippet={flutterSnippet}
            aiPrompt={aiPrompt}
            showAIPrompt={showAIPrompt}
          />
        </SettingsCard>
      </div>
    </PageContentWrapper>
  );
};
