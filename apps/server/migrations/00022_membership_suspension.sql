-- Suspending a member's access to one organization (owner decision D-AO01 of 2026-10-25).
--
-- suspended_at is null for a member with access and, once an owner or manager suspends them (the members page, or isActive: false through the v2 users API), the time it happened. A suspended member keeps their account, their sessions and their other organizations: the authorizer refuses them every decision in this organization except reading its name and their own membership and leaving it, the lookups leave its workspaces out, the API keys they created there stop authenticating, and its notifications stop. Restoring clears the column and gives everything back, keys included.
--
-- Until now the only way to cut a member off was users.is_active, which belongs to the account: the v2 users API of one organization wrote it, and so did the instance's operator, by hand. Nothing recorded which of the two turned an account off, so this migration leaves every account as it is: a deactivated account stays deactivated, whoever did it, and its operator turns it back on with `forma user activate` when it should return. Every membership of a deactivated account is suspended as well, so turning the account back on does not by itself give it an organization again: each organization restores its own member.
--
-- A nullable column without a default makes ADD COLUMN a catalog change: no existing row is rewritten. The update touches only the memberships of deactivated accounts, a handful of rows.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of memberships (most authorization decisions read it), and a migration should fail fast rather than queue every later request behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE memberships ADD COLUMN suspended_at timestamptz;

UPDATE memberships SET suspended_at = now()
    WHERE user_id IN (SELECT id FROM users WHERE NOT is_active);

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one has no notion of a suspension and would give a suspended member their access back. Every account with a suspended membership is deactivated instead, which is how that release cut a member off; a user suspended in one organization only loses the others too until someone reactivates the account.
UPDATE users SET is_active = false, updated_at = now()
    WHERE is_active AND EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = users.id AND m.suspended_at IS NOT NULL);

ALTER TABLE memberships DROP COLUMN suspended_at;
