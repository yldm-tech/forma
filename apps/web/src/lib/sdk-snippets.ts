// The SDK install snippets, with the workspace id and app URL filled in. Every snippet loads the SDK the same way, from the instance's /js/forma.umd.js through a script tag: no Forma package is published on npm (or for mobile and WordPress), so the page offers nothing that installs one.

export interface SdkSnippets {
  /** The script-tag loader for a plain page's <head>, also pasted into a Google Tag Manager Custom HTML tag and into index.html for React and Vue. */
  html: string;
  /** The same loader as a Next.js component, rendered through next/script. */
  nextjs: string;
  /** How app code calls the SDK the loader defines (window.forma), with the TypeScript declaration for it. */
  usage: string;
}

/** The methods the loader's stub accepts before the SDK has arrived. Each call is queued and replayed, in order, once it has: the SDK's own globalThis.forma replaces the stub when its script runs. setup is not among them: the loader calls it. */
export const SDK_STUB_METHODS = [
  "setUserId",
  "setEmail",
  "setAttribute",
  "setAttributes",
  "setLanguage",
  "track",
  "logout",
  "registerRouteChange",
  "setNonce",
  "setEmbeddedData",
  "clearEmbeddedData",
  "on",
  "off",
] as const;

/** Queued calls that do not go through the SDK's command queue, replayed before setup: an on() handler then sees forma_setup_successful, and the nonce and embedded data are in place before the first survey renders. Every other call is replayed after setup, since the command queue drops anything that arrives before it. */
const SDK_IMMEDIATE_METHODS = ["on", "off", "setNonce", "setEmbeddedData", "clearEmbeddedData"] as const;

/** The loader's JavaScript: ES5, because a GTM Custom HTML tag rejects newer syntax. A second copy on the page, or one running after the SDK is already there, does nothing. */
const loaderScript = (workspaceId: string, appUrl: string): string => `!function(){
    var appUrl = "${appUrl}";
    var workspaceId = "${workspaceId}";
    var f=window.forma=window.forma||{};if(f.setup||f.q)return;f.q=[];
    "${SDK_STUB_METHODS.join(" ")}".split(" ").forEach(function(m){f[m]=function(){f.q.push([m,arguments])}});
    var t=document.createElement("script");t.type="text/javascript",t.async=!0,t.src=appUrl+"/js/forma.umd.js";
    t.onload=function(){var s=window.forma,i=/^(${SDK_IMMEDIATE_METHODS.join("|")})$/,r=function(p){f.q.forEach(function(c){if(i.test(c[0])===p)s[c[0]].apply(s,c[1])})};r(!0);s.setup({workspaceId: workspaceId, appUrl: appUrl});r(!1)};
    var e=document.getElementsByTagName("script")[0];e.parentNode.insertBefore(t,e)}();`;

/** The install snippets for a workspace, pointing the SDK at appUrl. */
export const sdkSnippets = (workspaceId: string, appUrl: string): SdkSnippets => {
  const loader = loaderScript(workspaceId, appUrl);
  return {
    html: `<!-- START Forma Surveys -->
<script type="text/javascript">
${loader}
</script>
<!-- END Forma Surveys -->`,
    nextjs: `import Script from "next/script";

export default function FormaScript() {
  return (
    <Script id="forma-sdk" strategy="afterInteractive">
      {\`${loader}\`}
    </Script>
  );
}`,
    usage: `// forma.d.ts: lets TypeScript see the global the snippet defines.
interface Window {
  forma: { [method: string]: (...args: unknown[]) => unknown };
}

// In your app code, after the user signs in:
window.forma.setUserId(user.id);
window.forma.setAttributes({ plan: "pro" });

// When something worth a survey happens:
window.forma.track("upgrade_clicked");

// When the user signs out:
window.forma.logout();`,
  };
};

/** The SDK reference on the website, in the website's language closest to the app's: it has en, zh (Simplified) and ja. */
export const sdkReferenceUrl = (language: string): string => {
  const prefix = language.startsWith("ja") ? "/ja" : language.startsWith("zh") ? "/zh" : "";
  return `https://forma.yldm.ai${prefix}/developers/sdk/`;
};
