import { describe, expect, test } from "vitest";
import { findViolationsInSource, parseTenantModels } from "./check-tenant-scoping";

const SCHEMA = `
model Survey {
  id          String @id
  workspaceId String
}

model Membership {
  id             String @id
  organizationId String
}

model User {
  id    String @id
  email String
}
`;

const models = parseTenantModels(SCHEMA);

describe("parseTenantModels", () => {
  test("maps a tenant-scoped model to its column, under the name Prisma exposes", () => {
    expect(models.get("survey")).toBe("workspaceId");
    expect(models.get("membership")).toBe("organizationId");
  });

  test("leaves out a model with no tenant column", () => {
    expect(models.has("user")).toBe(false);
  });
});

describe("findViolationsInSource", () => {
  test("flags a read with no tenant constraint anywhere in its function", () => {
    const source = `export const all = async () => prisma.survey.findMany({ where: { status: "draft" } });`;

    expect(findViolationsInSource(source, models)).toEqual([
      { line: 1, model: "survey", operation: "findMany", column: "workspaceId" },
    ]);
  });

  test("accepts the column appearing anywhere in the function, not only in the call", () => {
    const source = [
      "export const scoped = async (id: string) => {",
      "  const where = { workspaceId: id };",
      "  return prisma.survey.findMany({ where });",
      "};",
    ].join("\n");

    expect(findViolationsInSource(source, models)).toEqual([]);
  });

  test("accepts a helper that takes tenant ids rather than naming the column", () => {
    const source = [
      "export const listed = async (workspaceIds: string[]) => {",
      "  return prisma.survey.findMany(buildQuery(workspaceIds));",
      "};",
    ].join("\n");

    expect(findViolationsInSource(source, models)).toEqual([]);
  });

  test("does not let one function's scoping cover the next function", () => {
    const source = [
      "export const scoped = async (workspaceId: string) => {",
      "  return prisma.survey.findMany({ where: { workspaceId } });",
      "};",
      "",
      "export const unscoped = async () => {",
      "  return prisma.survey.findMany({});",
      "};",
    ].join("\n");

    expect(findViolationsInSource(source, models).map((v) => v.line)).toEqual([6]);
  });

  test("honours an exemption on the line above", () => {
    const source = [
      "export const global = async () => {",
      "  // tenant-scope-exempt: instance-wide telemetry",
      "  return prisma.survey.count();",
      "};",
    ].join("\n");

    expect(findViolationsInSource(source, models)).toEqual([]);
  });

  test("requires a reason after the exemption marker", () => {
    const source = [
      "export const global = async () => {",
      "  // tenant-scope-exempt:",
      "  return prisma.survey.count();",
      "};",
    ].join("\n");

    expect(findViolationsInSource(source, models)).toHaveLength(1);
  });

  test("ignores findUnique, which takes a unique key rather than a filter", () => {
    const source = `export const one = async (id: string) => prisma.survey.findUnique({ where: { id } });`;

    expect(findViolationsInSource(source, models)).toEqual([]);
  });

  test("ignores a model that carries no tenant column", () => {
    const source = `export const users = async () => prisma.user.findMany({});`;

    expect(findViolationsInSource(source, models)).toEqual([]);
  });
});
