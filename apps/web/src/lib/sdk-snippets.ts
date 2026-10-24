// forma2's SDK install snippets (modules/workspaces/settings/(setup)/app-connection/page.tsx), with the workspace id and app URL filled in. The HTML snippet calls setup when the script has loaded rather than after forma2's fixed 500 ms, which a slow network outran, as the marketing site's snippet does.

export interface SdkSnippets {
  html: string;
  nextjs: string;
  react: string;
  vue: string;
  reactNative: string;
  android: string;
  swift: string;
  flutter: string;
}

/** The install snippets for a workspace, pointing the SDK at appUrl. */
export const sdkSnippets = (workspaceId: string, appUrl: string): SdkSnippets => ({
  html: `<!-- START Forma Surveys -->
<script type="text/javascript">
!function(){
    var appUrl = "${appUrl}";
    var workspaceId = "${workspaceId}";
var t=document.createElement("script");t.type="text/javascript",t.async=!0,t.src=appUrl+"/js/forma.umd.cjs";t.onload=function(){window.forma.setup({workspaceId: workspaceId, appUrl: appUrl})};var e=document.getElementsByTagName("script")[0];e.parentNode.insertBefore(t,e)}();
</script>
<!-- END Forma Surveys -->`,
  nextjs: `"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { useEffect } from "react";
import forma from "@forma/js";

export default function FormaProvider() {
  const pathname = usePathname();
  const searchParams = useSearchParams();

  useEffect(() => {
    forma.setup({ workspaceId: "${workspaceId}", appUrl: "${appUrl}" });
  }, []);

  useEffect(() => {
    forma?.registerRouteChange();
  }, [pathname, searchParams]);

  return null;
}`,
  react: `import forma from "@forma/js";

if (typeof window !== "undefined") {
  forma.setup({
    workspaceId: "${workspaceId}",
    appUrl: "${appUrl}",
  });
}`,
  vue: `import forma from "@forma/js";

if (typeof window !== "undefined") {
  forma.setup({
    workspaceId: "${workspaceId}",
    appUrl: "${appUrl}",
  });
}

export default forma;`,
  reactNative: `import Forma from "@forma/react-native";

const config = {
  workspaceId: "${workspaceId}",
  appUrl: "${appUrl}",
};

export default function App() {
  return (
    <>
      {/* Your app content */}
      <Forma initConfig={config} />
    </>
  );
}`,
  android: `val config = FormaConfig.Builder(
    "${appUrl}",
    "${workspaceId}"
)
  .setFragmentManager(supportFragmentManager)
  .build()

Forma.setup(this, config)`,
  swift: `import FormaSDK

let config = FormaConfig.Builder(
    appUrl: "${appUrl}",
    workspaceId: "${workspaceId}"
).build()

Forma.setup(with: config)`,
  flutter: `import 'package:forma/forma.dart';

Forma(
  appUrl: "${appUrl}",
  workspaceId: "${workspaceId}",
);`,
});
