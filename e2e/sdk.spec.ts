import { type Server, createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { expect, test } from "@playwright/test";
import { DEFAULT_QUESTION, finishOnboarding, post, signUpAndSignIn } from "./helpers";

// forma2's js.spec.ts: a website & app survey triggered on a page view, shown by the SDK (/js/forma.umd.cjs, packages/js-core) on a customer's page, answered there and found in the responses view.

/** A customer's page with the SDK snippet forma2's app-connection page hands out. */
const hostPage = (appUrl: string, workspaceId: string) => `<!doctype html>
<html lang="en">
  <head>
    <title>Customer site</title>
    <script type="text/javascript">
      !(function () {
        var t = document.createElement("script");
        (t.type = "text/javascript"), (t.async = !0), (t.src = "${appUrl}/js/forma.umd.cjs");
        var e = document.getElementsByTagName("script")[0];
        e.parentNode.insertBefore(t, e);
        t.onload = function () {
          window.forma.setup({ workspaceId: "${workspaceId}", appUrl: "${appUrl}" });
        };
      })();
    </script>
  </head>
  <body>
    <main><h1>A page with the Forma SDK</h1></main>
  </body>
</html>`;

let server: Server | undefined;

test.afterEach(async () => {
  await new Promise<void>((resolve) => (server ? server.close(() => resolve()) : resolve()));
  server = undefined;
});

test("shows an app survey through the SDK on a host page and records the answer", async ({
  page,
  baseURL,
}) => {
  const answer = `Answered in the widget ${Math.random().toString(36).slice(2, 8)}`;

  await signUpAndSignIn(page, baseURL!, "sdk");
  const { organizationId, workspaceId } = await finishOnboarding(page);

  const surveyId = await test.step("create a website & app survey triggered on every page view", async () => {
    // Set up over the APIs a customer's backend uses rather than in the editor: publishing an app survey from the editor creates its private segment with no filters, which the server refuses ("At least one filter is required"), and the editor's own "Capture new action" form refuses the rewrite's uuids. The v1 management API creates the private segment itself, as forma2's did.
    const action = await post<{ id: string }>(
      page.request,
      baseURL!,
      `/api/app/workspaces/${workspaceId}/action-classes`,
      {
        name: "New Session",
        type: "noCode",
        noCodeConfig: { type: "pageView", urlFilters: [] },
      }
    );
    const { key } = await post<{ key: string }>(
      page.request,
      baseURL!,
      `/api/app/organizations/${organizationId}/api-keys`,
      {
        label: "Playwright",
        workspacePermissions: [{ workspaceId, permission: "write" }],
        organizationAccess: { accessControl: { read: false, write: false } },
      }
    );
    const survey = {
      workspaceId,
      name: `App survey ${Date.now()}`,
      type: "app",
      status: "inProgress",
      blocks: [
        {
          id: "sdkblock",
          name: "Block 1",
          elements: [
            {
              id: "feedback",
              type: "openText",
              headline: { default: DEFAULT_QUESTION },
              required: true,
              inputType: "text",
              charLimit: { enabled: false },
            },
          ],
        },
      ],
      endings: [{ id: "sdkend", type: "endScreen", headline: { default: "Thank you!" } }],
      triggers: [{ actionClass: { id: action.id } }],
    };
    // The key's workspace grant reaches the authorization service a moment after it is created (the worker delivers the projection); until then the API answers 401.
    let created: { data: { id: string } } | undefined;
    await expect(async () => {
      const response = await page.request.post("/api/v1/management/surveys", {
        data: survey,
        headers: { "x-api-key": key },
      });
      expect(response.status()).toBe(200);
      created = (await response.json()) as { data: { id: string } };
    }).toPass();
    return created!.data.id;
  });

  const host = await test.step("serve a customer page carrying the SDK", async () => {
    server = createServer((_, res) => {
      res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
      res.end(hostPage(baseURL!, workspaceId));
    });
    await new Promise<void>((resolve) => server!.listen(0, "127.0.0.1", resolve));
    return `http://127.0.0.1:${(server!.address() as AddressInfo).port}/`;
  });

  await test.step("answer the survey in the widget", async () => {
    await page.goto(host);
    const widget = page.locator("#forma-modal-container");
    await expect(widget.getByRole("dialog")).toBeVisible();
    await widget.getByRole("textbox", { name: DEFAULT_QUESTION }).fill(answer);
    await widget.getByRole("button", { name: "Finish" }).click();
    await expect(widget.getByRole("heading", { name: "Thank you!" })).toBeVisible();
  });

  await test.step("find the response", async () => {
    await page.goto(`/workspaces/${workspaceId}/surveys/${surveyId}/responses`);
    const row = page.getByRole("main").getByRole("table").getByRole("row").filter({ hasText: answer });
    await expect(row).toHaveCount(1);
  });
});
