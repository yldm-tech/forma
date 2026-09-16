import { describe, expect, test } from "vitest";
import { classifyEndpoint, isExempt, isGuarded, touchesModels } from "./check-ee-entitlement";

const ROUTE = `import { authenticatedApiClient } from "@/modules/api/v2/auth/authenticated-api-client";`;

describe("touchesModels", () => {
  test("reports every gated model the source reaches", () => {
    const src = `prisma.contact.findMany(); prisma.segment.count();`;

    expect(touchesModels(src, ["contact", "segment", "surveyQuota"])).toEqual(["contact", "segment"]);
  });

  test("does not match a model whose name is a prefix of another", () => {
    expect(touchesModels(`prisma.contactAttributeKey.findMany();`, ["contact"])).toEqual([]);
  });
});

describe("isGuarded", () => {
  test("accepts either spelling of the guard", () => {
    expect(isGuarded(`await getIsContactsEnabled(id)`, ["getIsContactsEnabled"])).toBe(true);
    expect(isGuarded(`await checkContactsEnabledApiV2(id)`, ["checkContactsEnabled"])).toBe(true);
  });

  test("rejects source that never asks", () => {
    expect(isGuarded(`prisma.contact.findMany()`, ["getIsContactsEnabled"])).toBe(false);
  });
});

describe("isExempt", () => {
  test("needs a reason, not just the marker", () => {
    expect(isExempt(`// ee-entitlement-exempt: response data, not the feature`)).toBe(true);
    expect(isExempt(`// ee-entitlement-exempt:`)).toBe(false);
  });
});

describe("classifyEndpoint", () => {
  test("flags an endpoint whose colocated code reads gated data unguarded", () => {
    const findings = classifyEndpoint(ROUTE, `prisma.contact.findMany({ where: { workspaceId } });`);

    expect(findings).toEqual([{ endpoint: "", feature: "contacts", models: ["contact"] }]);
  });

  test("accepts a guard in the route even when the query is colocated", () => {
    const route = `${ROUTE}\nawait checkContactsEnabledApiV2(organizationId);`;

    expect(classifyEndpoint(route, `prisma.contact.findMany();`)).toEqual([]);
  });

  test("accepts a guard in the colocated code even when the route has none", () => {
    expect(classifyEndpoint(ROUTE, `await getIsContactsEnabled(id); prisma.contact.findMany();`)).toEqual([]);
  });

  test("an exemption silences the endpoint entirely", () => {
    const route = `// ee-entitlement-exempt: response data\n${ROUTE}`;

    expect(classifyEndpoint(route, `prisma.contact.findMany(); prisma.workflow.findMany();`)).toEqual([]);
  });

  test("reports each licensed feature the endpoint touches separately", () => {
    const findings = classifyEndpoint(ROUTE, `prisma.contact.findMany(); prisma.workflow.findMany();`);

    expect(findings.map((f) => f.feature)).toEqual(["contacts", "workflows"]);
  });

  test("says nothing about an endpoint that touches no gated model", () => {
    expect(classifyEndpoint(ROUTE, `prisma.survey.findMany();`)).toEqual([]);
  });
});
