import { ZUserEmail, ZUserName } from "@forma/types/user";
import { z } from "zod";

// The client-side checks of forma2's forms, with forma2's (English) zod messages; the server applies the same rules again.

const firstIssue = (schema: z.ZodType, value: unknown): string | null => {
  const result = schema.safeParse(value);
  return result.success ? null : (result.error.issues[0]?.message ?? "Invalid input");
};

/** ZUserName: trimmed, at least one character, letters, marks, digits and common name punctuation. */
export const userNameError = (name: string): string | null => firstIssue(ZUserName, name);

/** ZUserEmail, with the invite form's "Email is required" for an empty field. */
export const emailError = (email: string): string | null =>
  email.trim() === "" ? "Email is required" : firstIssue(ZUserEmail, email.trim());

export const workspaceNameError = (name: string): string | null =>
  name.trim() === "" ? "Workspace name cannot be empty" : null;

export const organizationNameError = (name: string): string | null =>
  name.trim() === "" ? "Organization name must be at least 1 character long" : null;

export const teamNameError = (name: string): string | null => (name.trim() === "" ? "Team name is required" : null);
