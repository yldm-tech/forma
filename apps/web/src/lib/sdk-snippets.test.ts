import { runInNewContext } from "node:vm";
import { expect, test } from "vite-plus/test";
import { sdkSnippets } from "./sdk-snippets";

test("every snippet names the workspace and the app URL", () => {
  const snippets = sdkSnippets("0190a000-0000-7000-8000-000000000002", "https://forma.example.com");
  for (const snippet of Object.values(snippets)) {
    expect(snippet).toContain("0190a000-0000-7000-8000-000000000002");
    expect(snippet).toContain("https://forma.example.com");
  }
  expect(snippets.html).toContain('t.src=appUrl+"/js/forma.umd.cjs"');
});

test("the HTML snippet sets the SDK up once its script has loaded, not after a fixed delay", () => {
  const { html } = sdkSnippets("ws", "https://forma.example.com");
  expect(html).toContain(
    "t.onload=function(){window.forma.setup({workspaceId: workspaceId, appUrl: appUrl})}"
  );
  expect(html).not.toContain("setTimeout");
});

test("the HTML snippet runs: setup is called only after the script loads, however late", () => {
  const { html } = sdkSnippets("ws-1", "https://forma.example.com");
  const source = html.replace(/<!--[^>]*-->/g, "").replace(/<\/?script[^>]*>/g, "");
  const inserted: { onload?: () => void; src?: string }[] = [];
  const setup: unknown[] = [];
  const window = { forma: undefined as undefined | { setup: (config: unknown) => void } };
  const document = {
    createElement: () => {
      const element = {} as { onload?: () => void; src?: string };
      inserted.push(element);
      return element;
    },
    getElementsByTagName: () => [{ parentNode: { insertBefore: () => undefined } }],
  };
  runInNewContext(source, { window, document });
  expect(inserted).toHaveLength(1);
  expect(inserted[0]?.src).toBe("https://forma.example.com/js/forma.umd.cjs");
  // The script arrives late: nothing has run yet, and nothing throws for the missing window.forma.
  window.forma = { setup: (config) => setup.push(config) };
  inserted[0]?.onload?.();
  expect(setup).toEqual([{ workspaceId: "ws-1", appUrl: "https://forma.example.com" }]);
});
