-- Suspending a member's access to one organization (owner decision D-AO01 of 2026-10-25).
--
-- suspended_at is null for a member with access and, once an owner or manager suspends them (the members page, or isActive: false through the v2 users API), the time it happened. A suspended member keeps their account, their sessions and their other organizations: the authorizer refuses them every decision in this organization except reading its name and their own membership and leaving it, the lookups leave its workspaces out, the API keys they created there stop authenticating, and its notifications stop. Restoring clears the column and gives everything back, keys included.
--
-- Until now the only way to cut a member off was users.is_active, which belongs to the account: the v2 users API of one organization wrote it, and locked the user out of every organization they belong to. An account deactivated that way that still has memberships becomes a suspension of each of them, and the account is active again: the user keeps the access they had (none, in every organization they belonged to), can sign in, and is told which organizations suspended them. An inactive account without any membership stays inactive. users.is_active stays as the instance's own switch; no API writes it any more.
--
-- A nullable column without a default makes ADD COLUMN a catalog change: no existing row is rewritten. The two updates touch only deactivated accounts and their memberships, a handful of rows.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of memberships (most authorization decisions read it), and a migration should fail fast rather than queue every later request behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE memberships ADD COLUMN suspended_at timestamptz;

UPDATE memberships SET suspended_at = now()
    WHERE user_id IN (SELECT id FROM users WHERE NOT is_active);

-- The users trigger enqueues the projection of each reactivated account, which writes its relationships back to SpiceDB; the suspensions above keep the authorizer refusing them.
UPDATE users SET is_active = true, updated_at = now()
    WHERE NOT is_active AND EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = users.id);

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one has no notion of a suspension and would give a suspended member their access back. Every account with a suspended membership is deactivated instead, which is how that release cut a member off; a user suspended in one organization only loses the others too until someone reactivates the account.
UPDATE users SET is_active = false, updated_at = now()
    WHERE is_active AND EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = users.id AND m.suspended_at IS NOT NULL);

ALTER TABLE memberships DROP COLUMN suspended_at;
