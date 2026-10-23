import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { LAST_USED_KEY } from "../last-used";
import { SSOButtons } from "./sso-buttons";

// forma2's SSOOptions and SamlButton: SAML is offered last, remembered as "Saml", and a missing connection is forma2's saml_connection_error toast.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

beforeEach(() => localStorage.clear());

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** POST /api/app/auth/sso/sign-in answering with the given response; the request bodies are recorded. */
const stubSignIn = (answer: Response) => {
  const bodies: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (path !== "/api/app/auth/sso/sign-in" || init.method !== "POST")
        throw new Error(`unexpected ${path}`);
      bodies.push(JSON.parse(init.body as string));
      return answer;
    })
  );
  return bodies;
};

const renderButtons = (onError = vi.fn(), lastUsed = "") => {
  render(
    <I18nextProvider i18n={i18n}>
      <SSOButtons
        providers={["google", "openid", "saml"]}
        returnToUrl="/workspaces/w1"
        source="signin"
        lastUsed={lastUsed}
        onError={onError}
      />
    </I18nextProvider>
  );
  return onError;
};

describe("SSOButtons with SAML", () => {
  test("offers SAML after the OAuth providers, as the server lists them, and marks it when used last", () => {
    renderButtons(vi.fn(), "Saml");

    const labels = screen.getAllByRole("button").map((button) => button.textContent);
    expect(labels).toEqual([
      i18n.t("auth.continue_with_google"),
      i18n.t("auth.continue_with_openid"),
      `${i18n.t("auth.continue_with_saml")}${i18n.t("auth.last_used")}`,
    ]);
  });

  test("starts SAML with the return path and leaves for the identity provider", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...globalThis.location, assign });
    const bodies = stubSignIn(
      json(200, { url: "https://idp.example.com/sso/saml?SAMLRequest=x&RelayState=y" })
    );
    renderButtons();

    fireEvent.click(screen.getByRole("button", { name: i18n.t("auth.continue_with_saml") }));

    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith("https://idp.example.com/sso/saml?SAMLRequest=x&RelayState=y")
    );
    expect(bodies).toEqual([{ provider: "saml", callbackUrl: "/workspaces/w1?source=signin" }]);
    expect(localStorage.getItem(LAST_USED_KEY)).toBe("Saml");
  });

  test("SAML without a usable connection shows forma2's saml_connection_error and stays on the page", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...globalThis.location, assign });
    stubSignIn(
      json(404, { code: "saml_connection_error", message: "No SAML connection is configured.", details: {} })
    );
    const onError = renderButtons();

    fireEvent.click(screen.getByRole("button", { name: i18n.t("auth.continue_with_saml") }));

    await waitFor(() => expect(onError).toHaveBeenCalledWith(i18n.t("auth.saml_connection_error")));
    expect(assign).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: i18n.t("auth.continue_with_saml") }).hasAttribute("disabled")
    ).toBe(false);
  });
});
