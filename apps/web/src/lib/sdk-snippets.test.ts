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
