import { useMutation, useQueryClient } from "@tanstack/react-query";
import { parse } from "csv-parse/browser/esm/sync";
import type { TFunction } from "i18next";
import { ArrowUpFromLineIcon } from "lucide-react";
import { type DragEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { DialogFooter } from "../../components/ui/dialog";
import { ApiError, api } from "../../lib/api";
import { sniffDelimiter } from "../../lib/contacts-csv";
import { errorMessage } from "../../lib/errors";
import { type InviteFailureReason, inviteFailureMessage, inviteFailureReason } from "../../lib/invites";
import { type Invitee, inviteKeys } from "../../lib/queries/invites";
import type { OrganizationRole } from "../../lib/roles";
import { toast } from "../../lib/toast";
import { emailError, userNameError } from "../../lib/validation";

// forma2's bulk invite tab (modules/organization/settings/teams/components/invite-member/bulk-invite-tab.tsx) and the bulk half of its handleAddMembers: a CSV of members, previewed, checked against the organization's teams, then invited in one request (POST /api/app/organizations/{id}/invites/bulk, apps/server/internal/invite).

/** One CSV row by header. A missing or misnamed column is absent, so a cell is never assumed to be a string (forma2's ENG-1596). */
export type BulkCsvRow = Record<string, string | undefined>;

/** The template linked from the tab; the file is served from apps/web/public. */
export const BULK_INVITE_TEMPLATE_PATH = "/sample-csv/forma-organization-members-template.csv";

const PREVIEW_ROW_LIMIT = 11;
/** forma2's invite role enum. */
const ROLES: readonly string[] = ["owner", "manager", "member", "billing"] satisfies OrganizationRole[];
const MAX_FAILURE_LINES = 5;

/** The first non-empty value among a column's accepted headers, or "" so validation rejects the row instead of a crash. */
export const readCell = (row: BulkCsvRow, ...keys: string[]): string => {
  for (const key of keys) {
    const value = row[key];
    if (value !== undefined && value !== null && value !== "") return value;
  }
  return "";
};

/** A Teams cell's team names, separated by commas or pipes. */
export const parseTeamCell = (cell: string | undefined): string[] => {
  if (!cell) return [];
  return cell
    .split(/[,|]/)
    .map((team) => team.trim())
    .filter((team) => team.length > 0);
};

/** Reads the file's rows by its header row. The delimiter is detected from the header as for the contacts import, empty lines are skipped, and a row with too few or too many fields still parses, as Papa Parse's FieldMismatch was not fatal for forma2; anything else unreadable throws. */
export const parseBulkCsv = (text: string): BulkCsvRow[] => {
  const withoutBom = text.startsWith("﻿") ? text.slice(1) : text;
  const headerLine = withoutBom.split(/\r?\n|\r/, 1)[0] ?? "";
  return parse(withoutBom, {
    columns: true,
    skip_empty_lines: true,
    relax_column_count: true,
    delimiter: sniffDelimiter(headerLine),
  }) as BulkCsvRow[];
};

export type BulkInviteCheck =
  | { invitees: Invitee[] }
  | { unknownTeams: string[] }
  | { billingEmails: string[] }
  | { invalid: true };

/** forma2's onImport checks, in its order: every team named must exist (matched by name, ignoring case), the billing role only where it is offered, then every row a valid invitee (forma2's ZInvitees). */
export const checkBulkInvitees = (
  rows: BulkCsvRow[],
  teams: { id: string; name: string }[],
  billingRoleAllowed: boolean
): BulkInviteCheck => {
  const teamByName = new Map(teams.map((team) => [team.name.trim().toLowerCase(), team]));
  const unknownTeams = new Set<string>();
  const billingEmails = new Set<string>();
  const members = rows.map((row) => {
    const email = readCell(row, "Email Address", "email").trim();
    const role = readCell(row, "Organization Role", "Role", "role").trim().toLowerCase();
    if (!billingRoleAllowed && role === "billing") billingEmails.add(email);
    const teamIds: string[] = [];
    for (const name of parseTeamCell(readCell(row, "Teams", "teams"))) {
      const match = teamByName.get(name.toLowerCase());
      if (!match) unknownTeams.add(name);
      else if (!teamIds.includes(match.id)) teamIds.push(match.id);
    }
    return { name: readCell(row, "Full Name", "name").trim(), email, role, teamIds };
  });
  if (unknownTeams.size > 0) return { unknownTeams: [...unknownTeams] };
  if (billingEmails.size > 0) return { billingEmails: [...billingEmails] };
  const valid = members.every(
    (member) => !userNameError(member.name) && !emailError(member.email) && ROLES.includes(member.role)
  );
  if (!valid) return { invalid: true };
  return { invitees: members.map((member) => ({ ...member, role: member.role as OrganizationRole })) };
};

/** forma2's formatInviteFailureMessages: one line per failed invitee, at most five, then how many more. */
export const inviteFailureLines = (
  t: TFunction,
  failures: { email: string; failureReason: InviteFailureReason }[]
): string => {
  const lines = failures
    .slice(0, MAX_FAILURE_LINES)
    .map((failure) => inviteFailureMessage(t, failure.email, failure.failureReason));
  if (failures.length > MAX_FAILURE_LINES)
    lines.push(
      t("workspace.settings.general.invite_failures_more", { count: failures.length - MAX_FAILURE_LINES })
    );
  return lines.join("\n");
};

interface BulkResult {
  email: string;
  success: boolean;
  failureReason?: string;
}

/** Invites the rows: one row as a single invite, more in one bulk request, as forma2's handleAddMembers did. Resolves whether anyone was invited, which closes the dialog. */
const useInviteRows = (organizationId: string) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const invite = useMutation({
    mutationFn: async (invitees: Invitee[]): Promise<BulkResult[]> => {
      const rows = invitees.map((invitee) => ({ ...invitee, email: invitee.email.toLowerCase() }));
      if (rows.length === 1 && rows[0]) {
        try {
          await api.post(`/api/app/organizations/${organizationId}/invites`, rows[0]);
          return [{ email: rows[0].email, success: true }];
        } catch (error) {
          const reason = error instanceof ApiError ? inviteFailureReason(error.code) : "unknown";
          if (reason === "unknown") throw error;
          return [{ email: rows[0].email, success: false, failureReason: reason }];
        }
      }
      const res = await api.post<{ results: BulkResult[] }>(
        `/api/app/organizations/${organizationId}/invites/bulk`,
        { invitees: rows }
      );
      return res.results;
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: inviteKeys.list(organizationId) }),
  });

  return async (invitees: Invitee[]): Promise<boolean> => {
    const toastId = toast.loading(
      t("workspace.settings.general.inviting_member", { count: invitees.length })
    );
    let results: BulkResult[];
    try {
      results = await invite.mutateAsync(invitees);
    } catch (error) {
      toast.error(errorMessage(t, error), { id: toastId });
      return false;
    }
    const failed = results
      .filter((result) => !result.success)
      .map((result) => ({
        email: result.email,
        failureReason: inviteFailureReason(result.failureReason ?? ""),
      }));
    const invited = results.length - failed.length;
    if (invited === 0) {
      toast.error(inviteFailureLines(t, failed), { id: toastId });
      return false;
    }
    toast.success(
      invitees.length === 1
        ? t("workspace.settings.general.member_invited_successfully")
        : t("workspace.settings.general.members_invited_successfully", { count: invited }),
      { id: toastId }
    );
    if (failed.length > 0) toast.error(inviteFailureLines(t, failed));
    return true;
  };
};

/** The CSV tab of the invite member dialog, for owners and managers. */
export const BulkInviteTab = ({
  organizationId,
  teams,
  billingRoleAllowed,
  onCancel,
  onDone,
}: {
  organizationId: string;
  teams: { id: string; name: string }[];
  billingRoleAllowed: boolean;
  onCancel: () => void;
  onDone: () => void;
}) => {
  const { t } = useTranslation();
  const inviteRows = useInviteRows(organizationId);
  const [file, setFile] = useState<File>();
  const [rows, setRows] = useState<BulkCsvRow[]>([]);
  const [error, setError] = useState("");
  const [importing, setImporting] = useState(false);

  const pickFile = async (picked: File | undefined) => {
    if (!picked) return;
    setRows([]);
    if (!picked.name.toLowerCase().endsWith(".csv")) {
      setError(t("common.invalid_file_type"));
      return;
    }
    setError("");
    setFile(picked);
    try {
      setRows(parseBulkCsv(await picked.text()));
    } catch {
      setError(t("workspace.settings.general.please_check_csv_file"));
    }
  };

  const onDragOver = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
  };

  const onDrop = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    event.stopPropagation();
    void pickFile(event.dataTransfer.files[0]);
  };

  const reset = () => {
    setFile(undefined);
    setRows([]);
    setError("");
  };

  const onImport = async () => {
    if (!file || rows.length === 0 || importing) return;
    const check = checkBulkInvitees(rows, teams, billingRoleAllowed);
    if ("unknownTeams" in check) {
      setError(
        t("workspace.settings.general.bulk_invite_unknown_teams", { teams: check.unknownTeams.join(", ") })
      );
      return;
    }
    if ("billingEmails" in check) {
      setError(
        t("workspace.settings.general.bulk_invite_billing_not_supported", {
          emails: check.billingEmails.join(", "),
        })
      );
      return;
    }
    if ("invalid" in check) {
      setError(t("workspace.settings.general.please_check_csv_file"));
      return;
    }
    setImporting(true);
    try {
      if (await inviteRows(check.invitees)) onDone();
    } finally {
      setImporting(false);
    }
  };

  const extraRows = Math.max(rows.length - PREVIEW_ROW_LIMIT, 0);

  return (
    <>
      <div className="flex flex-col gap-4">
        {error && (
          <Alert variant="error">
            <AlertDescription className="break-words whitespace-normal">{error}</AlertDescription>
          </Alert>
        )}
        <div className="flex flex-col gap-2">
          <div className="rounded-md border-2 border-dashed border-slate-300 bg-slate-50 p-4">
            {file ? (
              <div className="flex flex-col items-center gap-3 py-2">
                <h3 className="font-medium text-slate-700">{file.name}</h3>
                {rows.length > 0 && (
                  <div className="max-h-[300px] w-full overflow-auto rounded-md border border-slate-300">
                    <CsvPreview rows={rows.slice(0, PREVIEW_ROW_LIMIT)} />
                  </div>
                )}
                <p className="text-xs text-slate-500">
                  {t("workspace.settings.general.bulk_invite_rows_detected", { count: rows.length })}
                  {extraRows > 0
                    ? ` · ${t("workspace.settings.general.bulk_invite_rows_more", { count: extraRows })}`
                    : ""}
                </p>
              </div>
            ) : (
              <label
                htmlFor="bulk-invite-file"
                className="relative flex cursor-pointer flex-col items-center justify-center rounded-lg hover:bg-slate-100"
                onDragOver={onDragOver}
                onDrop={onDrop}>
                <div className="flex flex-col items-center justify-center pt-5 pb-6">
                  <ArrowUpFromLineIcon className="h-6 text-slate-500" aria-hidden="true" />
                  <p className="mt-2 text-center text-sm text-slate-500">
                    <span className="font-semibold">{t("common.upload_input_description")}</span>
                  </p>
                  <input
                    type="file"
                    id="bulk-invite-file"
                    name="bulk-invite-file"
                    accept=".csv"
                    className="hidden"
                    onChange={(event) => void pickFile(event.target.files?.[0])}
                  />
                </div>
              </label>
            )}
          </div>
          {!file && (
            <div className="flex justify-start">
              <Button variant="secondary" asChild>
                <a href={BULK_INVITE_TEMPLATE_PATH} download target="_blank" rel="noopener noreferrer">
                  {t("workspace.settings.general.bulk_invite_download_template")}
                </a>
              </Button>
            </div>
          )}
        </div>
      </div>
      <DialogFooter>
        {file ? (
          <Button variant="secondary" type="button" onClick={reset}>
            {t("workspace.contacts.upload_contacts_modal_pick_different_file")}
          </Button>
        ) : (
          <Button variant="secondary" type="button" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        )}
        <Button
          onClick={() => void onImport()}
          disabled={!file || rows.length === 0 || importing}
          loading={importing}>
          {t("common.import")}
        </Button>
      </DialogFooter>
    </>
  );
};

/** forma2's CsvTable: the first rows under the file's own headers. */
const CsvPreview = ({ rows }: { rows: BulkCsvRow[] }) => {
  const columns = Object.keys(rows[0] ?? {});
  return (
    <table className="w-max min-w-full border-separate border-spacing-0 text-left text-xs">
      <thead>
        <tr className="bg-slate-100">
          {columns.map((column) => (
            <th
              key={column}
              scope="col"
              className="sticky top-0 min-w-[120px] border-b-2 border-slate-200 bg-slate-100 px-3 py-2 font-semibold">
              {column}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, index) => (
          // Rows are a fixed slice of the parsed file and never reorder.
          // oxlint-disable-next-line react/no-array-index-key
          <tr key={index} className="bg-white">
            {columns.map((column) => (
              <td key={column} className="border-b border-slate-200 px-3 py-2">
                {row[column] ?? ""}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
};
