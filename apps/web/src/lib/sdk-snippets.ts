// The SDK install snippets, with the workspace id and app URL filled in. Every snippet loads the SDK the same way, from the instance's /js/forma.umd.js through a script tag: no Forma package is published on npm (or for mobile and WordPress), so the page offers nothing that installs one.

export interface SdkSnippets {
  /** The script-tag loader for a plain page's <head>, also pasted into a Google Tag Manager Custom HTML tag and into index.html for React and Vue. */
  html: string;
  /** The same loader as a Next.js component, rendered through next/script with strategy="beforeInteractive": Next then puts it in the server HTML ahead of hydration, as the <head> snippet is, so window.forma exists before any component's effect runs. afterInteractive injects it from an effect inside <Script>, which runs after the effects of the components rendered before it, and a call from one of those throws because window.forma is not there yet. */
  nextjs: string;
  /** How app code calls the SDK the loader defines (window.forma), with the TypeScript declaration for it. */
  usage: string;
}

/** The methods the loader's stub accepts before the SDK has arrived. Each call is queued and replayed, in order, once it has: the SDK's own globalThis.forma replaces the stub when its script runs. After that the stub's methods forward to the SDK, so app code that kept a reference to window.forma, or to one of its methods, from before the load still reaches it. setup is not among them: the loader calls it. */
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
    var l;"${SDK_STUB_METHODS.join(" ")}".split(" ").forEach(function(m){f[m]=function(){if(l)return l[m].apply(l,arguments);f.q.push([m,arguments])}});
    var t=document.createElement("script");t.type="text/javascript",t.async=!0,t.src=appUrl+"/js/forma.umd.js";
    t.onload=function(){var s=window.forma,i=/^(${SDK_IMMEDIATE_METHODS.join("|")})$/,r=function(p){f.q.forEach(function(c){if(i.test(c[0])===p)s[c[0]].apply(s,c[1])})};r(!0);s.setup({workspaceId: workspaceId, appUrl: appUrl});l=s;r(!1);f.q.length=0};
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
    <Script id="forma-sdk" strategy="beforeInteractive">
      {\`${loader}\`}
    </Script>
  );
}`,
    usage: `// forma.d.ts: lets TypeScript see the global the snippet defines.
interface Window {
  forma: Record<
${SDK_STUB_METHODS.map((method) => `    | "${method}"`).join("\n")},
    (...args: unknown[]) => unknown
  >;
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

/** The website's path prefix for the language closest to the app's: it has en, zh (Simplified) and ja. */
const websitePrefix = (language: string): string =>
  language.startsWith("ja") ? "/ja" : language.startsWith("zh") ? "/zh" : "";

/** The SDK reference on the website, in the website's language closest to the app's. */
export const sdkReferenceUrl = (language: string): string =>
  `https://forma.yldm.ai${websitePrefix(language)}/developers/sdk/`;

/** The REST API and MCP reference on the website (the developers page's reference section), in the website's language closest to the app's. */
export const apiReferenceUrl = (language: string): string =>
  `https://forma.yldm.ai${websitePrefix(language)}/developers/#reference`;
