import { useState } from "react";
import { useTranslation } from "react-i18next";
import { type SSOProvider, authApi } from "../api";
import { ssoReturnToUrl } from "../callback-url";
import { type SignInMethod, rememberLastUsed } from "../last-used";
import { Button } from "./ui";

// Each provider's id in forma2's "last used" record.
const METHOD: Record<SSOProvider, SignInMethod> = {
  google: "Google",
  github: "Github",
  azuread: "Azure",
  openid: "OpenID",
};

/**
 * The single sign-on buttons, in the order the server lists the configured providers. A click asks the server to start the flow (it sets the state cookie) and leaves for the provider's authorization page; a failure there comes back to /auth/login?error=<code>.
 */
export function SSOButtons({
  providers,
  oidcDisplayName,
  returnToUrl,
  source,
  lastUsed,
  onError,
}: {
  providers: SSOProvider[];
  oidcDisplayName?: string;
  returnToUrl: string;
  source: "signin" | "signup";
  lastUsed: string;
  onError: (error: unknown) => void;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState<SSOProvider>();

  const labels: Record<SSOProvider, string> = {
    google: t("auth.continue_with_google"),
    github: t("auth.continue_with_github"),
    azuread: t("auth.continue_with_azure"),
    openid: oidcDisplayName
      ? t("auth.continue_with_oidc", { oidcDisplayName })
      : t("auth.continue_with_openid"),
  };

  const start = async (provider: SSOProvider) => {
    rememberLastUsed(METHOD[provider]);
    setPending(provider);
    try {
      const { url } = await authApi.startSSO(provider, ssoReturnToUrl(returnToUrl, source));
      window.location.assign(url);
    } catch (error) {
      setPending(undefined);
      onError(error);
    }
  };

  if (providers.length === 0) return null;
  return (
    <div className="space-y-2">
      {providers.map((provider) => (
        <Button
          key={provider}
          variant="secondary"
          className="w-full"
          loading={pending === provider}
          disabled={pending !== undefined}
          onClick={() => void start(provider)}>
          <span className="truncate">{labels[provider]}</span>
          {lastUsed === METHOD[provider] && (
            <span className="shrink-0 text-xs opacity-50">{t("auth.last_used")}</span>
          )}
        </Button>
      ))}
    </div>
  );
}
