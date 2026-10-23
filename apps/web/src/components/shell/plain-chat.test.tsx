import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { PLAIN_SCRIPT_ID, PLAIN_SCRIPT_SRC, PlainChat, type PlainSession } from "./plain-chat";

afterEach(() => {
  cleanup();
  document.getElementById(PLAIN_SCRIPT_ID)?.remove();
  vi.unstubAllGlobals();
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const session: PlainSession = {
  appId: "liveChatApp_123",
  userId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f",
  email: "john@example.com",
  name: "John Doe",
  emailHash: "831412b17524e7d41b2f9085360840f35558068db4efe31cebb6375fbd1ac0a8",
  activeCustomerLabelTypeId: "lt_paying",
};

const renderChat = (answer: Response) => {
  const fetch = vi.fn(async () => answer);
  vi.stubGlobal("fetch", fetch);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <PlainChat />
    </QueryClientProvider>
  );
  return fetch;
};

// forma2's app layout mounted PlainChat only when PLAIN_APP_ID was set.
describe("PlainChat", () => {
  test("loads nothing when the server has no Plain app", async () => {
    const fetch = renderChat(json(404, { code: "not_found", message: "Plain chat not found" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/app/plain/session", expect.anything()));
    expect(document.getElementById(PLAIN_SCRIPT_ID)).toBeNull();
  });

  test("loads Plain's script and initializes it for the signed-in customer", async () => {
    renderChat(json(200, session));
    await waitFor(() => expect(document.getElementById(PLAIN_SCRIPT_ID)).not.toBeNull());
    const script = document.getElementById(PLAIN_SCRIPT_ID) as HTMLScriptElement;
    expect(script.src).toBe(PLAIN_SCRIPT_SRC);

    const plain = {
      init: vi.fn(),
      update: vi.fn(),
      setCustomerDetails: vi.fn(),
      close: vi.fn(),
      isInitialized: () => false,
    };
    vi.stubGlobal("Plain", plain);
    script.onload?.(new Event("load"));
    expect(plain.init).toHaveBeenCalledWith(
      expect.objectContaining({
        appId: "liveChatApp_123",
        customerDetails: {
          email: "john@example.com",
          emailHash: session.emailHash,
          fullName: "John Doe",
          shortName: "John",
          externalId: session.userId,
        },
        threadDetails: { labelTypeIds: ["lt_paying"] },
      })
    );
  });
});
