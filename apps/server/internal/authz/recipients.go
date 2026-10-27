package authz

// MemberNotRefusedSQL is a SQL condition on the membership row whose alias is membership ("m"): true when the organization refuses that member neither way refusal.go names, so neither by a suspension nor by its two-factor requirement (once the grace period is over, a member who signs in with a password and has not enrolled). Every query that picks members to send an organization's responses or summaries to (notification alerts, the workflow send_email recipients, the weekly summary) uses it, so a member the app refuses is not mailed what the app would refuse to show them, and the three cannot drift apart again.
func MemberNotRefusedSQL(membership string) string {
	return `(` + membership + `.suspended_at IS NULL AND NOT EXISTS (SELECT 1 FROM organizations refusing_org WHERE refusing_org.id = ` + membership + `.organization_id
		AND refusing_org.two_factor_enforced_at <= now() AND ` + SecondFactorStatusSQL(membership+".user_id") + ` = '` + SecondFactorDisabled + `'))`
}
