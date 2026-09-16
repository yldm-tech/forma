import type { Session, TAuthenticationApiKey } from "@forma/types/auth";
import type { TApiAuditLog } from "@/lib/api/with-api-logging";

export type TV3Authentication = TAuthenticationApiKey | Session | null;
export type TV3AuditLog = TApiAuditLog;
