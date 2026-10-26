import { describe, expect, test } from "vite-plus/test";
import type { TWorkflowDefinition, TWorkflowHttpRequestActionNode } from "../definition";
import {
  adoptSealedSecrets,
  clearSecretsOnOriginChange,
  generateSigningSecret,
  originOf,
} from "./action-config";

const config = (overrides: Partial<TWorkflowHttpRequestActionNode["config"]> = {}) => ({
  method: "POST" as const,
  url: "https://api.example.com/a",
  headers: [
    { name: "Authorization", secret: { sealed: "s1", hint: "…ijkl" } },
    { name: "X-Plain", value: "1" },
    { name: "X-New", secret: { value: "typed" } },
  ],
  body: "",
  signingSecret: { sealed: "s2" },
  ...overrides,
});

describe("clearSecretsOnOriginChange", () => {
  test("keeps sealed secrets while the URL stays on its origin", () => {
    expect(clearSecretsOnOriginChange(config(), "https://API.example.com/other?x=1")).toEqual({
      config: { url: "https://API.example.com/other?x=1" },
      cleared: false,
    });
  });

  test("empties sealed secrets, and only those, when the host, scheme or port changes", () => {
    for (const url of [
      "https://evil.example/a",
      "http://api.example.com/a",
      "https://api.example.com:8443/a",
    ]) {
      const next = clearSecretsOnOriginChange(config(), url);
      expect(next.cleared).toBe(true);
      expect(next.config).toEqual({
        url,
        headers: [
          { name: "Authorization", secret: {} },
          { name: "X-Plain", value: "1" },
          { name: "X-New", secret: { value: "typed" } },
        ],
        signingSecret: {},
      });
    }
  });

  test("changes nothing else without a sealed secret", () => {
    const plain = config({ headers: [{ name: "X-Plain", value: "1" }], signingSecret: null });
    expect(clearSecretsOnOriginChange(plain, "https://elsewhere.example")).toEqual({
      config: { url: "https://elsewhere.example" },
      cleared: false,
    });
  });
});

test("originOf", () => {
  expect(originOf("https://API.example.com:8443/a?b")).toBe("https://api.example.com:8443");
  expect(originOf("not a url")).toBe("");
});

test("generateSigningSecret is a Standard Webhooks secret of 32 random bytes", () => {
  const secret = generateSigningSecret();
  expect(secret).toMatch(/^whsec_[A-Za-z0-9+/]{43}=$/);
  expect(atob(secret.slice("whsec_".length))).toHaveLength(32);
  expect(generateSigningSecret()).not.toBe(secret);
});

describe("adoptSealedSecrets", () => {
  const httpNode = (cfg: Partial<TWorkflowHttpRequestActionNode["config"]>) => ({
    id: "http-1",
    type: "action" as const,
    actionType: "http_request" as const,
    config: {
      method: "POST" as const,
      url: "https://api.crm.example/a",
      headers: [],
      body: "",
      signingSecret: null,
      ...cfg,
    },
  });
  const slackNode = (webhookUrl: { value?: string; sealed?: string; hint?: string }) => ({
    id: "slack-1",
    type: "action" as const,
    actionType: "slack_message" as const,
    config: { webhookUrl, message: "Hi" },
  });
  const doc = (...nodes: unknown[]) =>
    ({
      schemaVersion: 1,
      entryNodeId: "t",
      trigger: null,
      nodes,
      edges: [],
    }) as unknown as TWorkflowDefinition;

  const sent = doc(
    httpNode({
      headers: [
        { name: "Authorization", secret: { value: "Bearer token-123456" } },
        { name: "X-Plain", value: "1" },
      ],
      signingSecret: { value: "whsec_typed" },
    }),
    slackNode({ value: "https://hooks.slack.com/services/T/B/x" })
  );
  const persisted = doc(
    httpNode({
      headers: [
        { name: "Authorization", secret: { sealed: "c2VhbGVk", hint: "…3456" } },
        { name: "X-Plain", value: "1" },
      ],
      signingSecret: { sealed: "c2lnbmluZw==", hint: "…yped" },
    }),
    slackNode({ sealed: "c2xhY2s=", hint: "hooks.slack.com/…/B/x" })
  );

  test("replaces the header secrets and webhook URLs that were sent with their sealed form, and keeps the signing secret in view", () => {
    const adopted = adoptSealedSecrets(sent, sent, persisted);
    expect(adopted.nodes).toEqual([
      httpNode({
        headers: [
          { name: "Authorization", secret: { sealed: "c2VhbGVk", hint: "…3456" } },
          { name: "X-Plain", value: "1" },
        ],
        signingSecret: { value: "whsec_typed" },
      }),
      slackNode({ sealed: "c2xhY2s=", hint: "hooks.slack.com/…/B/x" }),
    ]);
  });

  test("so a host change after the save asks for the header secret again", () => {
    const adopted = adoptSealedSecrets(sent, sent, persisted);
    const http = adopted.nodes[0] as TWorkflowHttpRequestActionNode;
    const next = clearSecretsOnOriginChange(http.config, "https://webhook.site/x");
    expect(next.cleared).toBe(true);
    expect(next.config.headers?.[0]).toEqual({ name: "Authorization", secret: {} });
  });

  test("keeps a value typed since the save, a header that moved, and a URL now on another origin", () => {
    const retyped = doc(
      httpNode({
        headers: [
          { name: "Authorization", secret: { value: "Bearer token-999" } },
          { name: "X-Plain", value: "1" },
        ],
        signingSecret: { value: "whsec_typed" },
      }),
      slackNode({ value: "https://hooks.slack.com/services/T/B/y" })
    );
    expect(adoptSealedSecrets(retyped, sent, persisted)).toBe(retyped);

    const renamed = doc(
      httpNode({
        headers: [{ name: "X-Api-Key", secret: { value: "Bearer token-123456" } }],
        signingSecret: { value: "whsec_typed" },
      })
    );
    expect(adoptSealedSecrets(renamed, sent, persisted)).toBe(renamed);

    const moved = doc(
      httpNode({
        url: "https://webhook.site/x",
        headers: [
          { name: "Authorization", secret: { value: "Bearer token-123456" } },
          { name: "X-Plain", value: "1" },
        ],
        signingSecret: { value: "whsec_typed" },
      })
    );
    expect(adoptSealedSecrets(moved, sent, persisted)).toBe(moved);
  });
});
