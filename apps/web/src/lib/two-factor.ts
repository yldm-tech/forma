/** A backup code as forma2 showed it, split in two halves: abcde-12345. */
export const formatBackupCode = (code: string): string => `${code.slice(0, 5)}-${code.slice(5, 10)}`;

/** The second factor to send when disabling two-factor authentication: exactly one of an authenticator code or a backup code, as forma2's form required ("Please provide either the code OR the backup code"). */
export const disableTwoFactorFactor = (
  code: string,
  backupCode: string
): { code: string } | { backupCode: string } | null => {
  const trimmedCode = code.trim();
  const trimmedBackup = backupCode.trim();
  if (trimmedCode && !trimmedBackup) return /^\d{6}$/.test(trimmedCode) ? { code: trimmedCode } : null;
  if (trimmedBackup && !trimmedCode) return { backupCode: trimmedBackup };
  return null;
};
