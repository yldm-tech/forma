package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// sessionUpdateAge is Better Auth's updateAge as forma2 configured it: a session is extended once it is this old relative to a full lifetime. forma2 set it to one day, equal to the default SESSION_MAX_AGE, so with defaults a session is never extended before it ends; a longer SESSION_MAX_AGE makes it slide.
const sessionUpdateAge = 24 * time.Hour

// sessionActivityInterval is how stale a session's updated_at may get while it is in use: a request that finds it older writes the time again, so the sessions list can show when each session was last active without a write on every request.
const sessionActivityInterval = 5 * time.Minute

// userAgentMaxLength bounds what is stored of the caller's User-Agent header.
const userAgentMaxLength = 512

// clientMeta is what a session records about the browser that created it.
type clientMeta struct {
	IP        string
	UserAgent string
}

func clientMetaFrom(c *gin.Context) clientMeta {
	ua := c.Request.UserAgent()
	if len(ua) > userAgentMaxLength {
		ua = ua[:userAgentMaxLength]
	}
	return clientMeta{IP: httpapi.ClientIP(c.Request.Context()), UserAgent: ua}
}

// issuedToken is an opaque token handed to the browser in a cookie, with when it stops working and the cookie lifetime that matches.
type issuedToken struct {
	Value     string
	ExpiresAt time.Time
	MaxAge    time.Duration
}

// Sessions stores browser sessions in PostgreSQL, keyed by the SHA-256 of an opaque cookie token, and implements httpapi.SessionAuth.
//
// forma2 also cached sessions in Redis and in a signed five-minute cookie, which let a revoked session keep working for up to five minutes. Every request here reads the session together with the user's is_active flag in one indexed query, which is the read forma2 made anyway to re-check is_active, so a revocation or deactivation takes effect on the next request.
type Sessions struct {
	db      *gorm.DB
	cookies cookieSettings
	maxAge  time.Duration
	log     *slog.Logger
	now     func() time.Time
}

func NewSessions(cfg config.Config, db *gorm.DB, log *slog.Logger) *Sessions {
	return &Sessions{
		db:      db,
		cookies: newCookieSettings(cfg),
		maxAge:  time.Duration(cfg.SessionMaxAge) * time.Second,
		log:     log,
		now:     time.Now,
	}
}

// create stores a new session for the user and returns its cookie token.
func (s *Sessions) create(ctx context.Context, db *gorm.DB, userID uuid.UUID, meta clientMeta) (issuedToken, error) {
	token, err := secret.NewToken()
	if err != nil {
		return issuedToken{}, err
	}
	now := s.now()
	expires := now.Add(s.maxAge)
	var ip any
	if _, ok := httpapi.CanonicalIP(meta.IP); ok {
		ip = meta.IP
	}
	if err := db.WithContext(ctx).Exec(
		`INSERT INTO sessions (user_id, token_hash, expires_at, ip_address, user_agent, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, secret.HashToken(token), expires, ip, nullIfEmpty(meta.UserAgent), now, now,
	).Error; err != nil {
		return issuedToken{}, fmt.Errorf("create session: %w", err)
	}
	return issuedToken{Value: token, ExpiresAt: expires, MaxAge: s.maxAge}, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// session is a valid session of an active user.
type session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
	// refreshed is set when this request extended the session, so the cookie must be re-sent.
	refreshed bool
}

// validate resolves a cookie token to its session. It returns nil for an unknown or expired token and for a user who is deactivated; an expired session is deleted on sight. A session in use has its updated_at kept within sessionActivityInterval of now, which the sessions list shows as its last activity.
func (s *Sessions) validate(ctx context.Context, token string) (*session, error) {
	if token == "" {
		return nil, nil
	}
	var rows []struct {
		ID        uuid.UUID
		UserID    uuid.UUID
		ExpiresAt time.Time
		UpdatedAt time.Time
		IsActive  bool
	}
	err := s.db.WithContext(ctx).Raw(
		`SELECT s.id, s.user_id, s.expires_at, s.updated_at, u.is_active FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`,
		secret.HashToken(token),
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	now := s.now()
	if !row.ExpiresAt.After(now) {
		if err := s.db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE id = ?`, row.ID).Error; err != nil {
			return nil, fmt.Errorf("delete expired session: %w", err)
		}
		return nil, nil
	}
	if !row.IsActive {
		return nil, nil
	}
	sess := &session{ID: row.ID, UserID: row.UserID, ExpiresAt: row.ExpiresAt}
	// Better Auth's rule: extend once expiresAt - expiresIn + updateAge has passed.
	if !row.ExpiresAt.Add(-s.maxAge).Add(sessionUpdateAge).After(now) {
		sess.ExpiresAt = now.Add(s.maxAge)
		if err := s.db.WithContext(ctx).Exec(
			`UPDATE sessions SET expires_at = ?, updated_at = ? WHERE id = ?`, sess.ExpiresAt, now, row.ID,
		).Error; err != nil {
			return nil, fmt.Errorf("refresh session: %w", err)
		}
		sess.refreshed = true
	} else if now.Sub(row.UpdatedAt) >= sessionActivityInterval {
		if err := s.db.WithContext(ctx).Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, now, row.ID).Error; err != nil {
			return nil, fmt.Errorf("record session activity: %w", err)
		}
	}
	return sess, nil
}

// delete ends one session.
func (s *Sessions) delete(ctx context.Context, id uuid.UUID) error {
	if err := s.db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE id = ?`, id).Error; err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// userSession is one of a user's sessions as the sessions list shows it. LastActiveAt is updated_at, which validate keeps within sessionActivityInterval of the session's last request. IPAddress and UserAgent are what the browser sent when the session was created, and are nil when it sent none.
type userSession struct {
	ID           uuid.UUID
	CreatedAt    time.Time
	LastActiveAt time.Time
	ExpiresAt    time.Time
	IPAddress    *string
	UserAgent    *string
}

// sessionIPv6PrefixBits is how much of an IPv6 client address httpapi.CanonicalIP keeps, and so the network a session's IPv6 address stands for.
const sessionIPv6PrefixBits = 64

// listFor returns the user's sessions that have not expired: current (the caller's own) first, then the most recently active. An IPv6 address is stored reduced to its /64, so it is reported as that network ("2001:db8:1:2::/64") rather than as the zero host of it.
func (s *Sessions) listFor(ctx context.Context, userID, current uuid.UUID) ([]userSession, error) {
	var rows []userSession
	err := s.db.WithContext(ctx).Raw(
		`SELECT id, created_at, updated_at AS last_active_at, expires_at, host(ip_address) AS ip_address, user_agent
		 FROM sessions WHERE user_id = ? AND expires_at > ?
		 ORDER BY id = ? DESC, updated_at DESC, id DESC`,
		userID, s.now(), current,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	for i := range rows {
		if rows[i].IPAddress == nil {
			continue
		}
		if addr, err := netip.ParseAddr(*rows[i].IPAddress); err == nil && addr.Is6() {
			network := netip.PrefixFrom(addr, sessionIPv6PrefixBits).Masked().String()
			rows[i].IPAddress = &network
		}
	}
	return rows, nil
}

// deleteFor ends one of the user's sessions and reports whether the user had it.
func (s *Sessions) deleteFor(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	res := s.db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE id = ? AND user_id = ?`, id, userID)
	if res.Error != nil {
		return false, fmt.Errorf("delete session: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// End ends the request's session and clears the browser's session cookie, for a sign-out another module completes: the authorization server's RP-initiated logout.
func (s *Sessions) End(c *gin.Context, id uuid.UUID) error {
	if err := s.delete(c.Request.Context(), id); err != nil {
		return err
	}
	s.cookies.clear(c, s.cookies.session)
	return nil
}

// RevokeAll ends every session of the user except keep (uuid.Nil keeps none) and returns how many it ended. Pass the transaction of the change that requires it, or nil to use the pool.
func (s *Sessions) RevokeAll(ctx context.Context, tx *gorm.DB, userID, keep uuid.UUID) (int64, error) {
	db := s.db
	if tx != nil {
		db = tx
	}
	res := db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keep)
	if res.Error != nil {
		return 0, fmt.Errorf("revoke sessions: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		s.log.InfoContext(ctx, "Revoked user sessions", "userId", userID.String(), "revoked", res.RowsAffected)
	}
	return res.RowsAffected, nil
}

// authenticate reads the session cookie and, for a valid session, records the actor and re-sends the cookie when the session was extended.
func (s *Sessions) authenticate(c *gin.Context) (bool, error) {
	// A missing cookie reads as the empty token, which validate treats as no session.
	token, _ := c.Cookie(s.cookies.session)
	sess, err := s.validate(c.Request.Context(), token)
	if err != nil || sess == nil {
		return false, err
	}
	if sess.refreshed {
		s.cookies.setSession(c, issuedToken{Value: token, ExpiresAt: sess.ExpiresAt, MaxAge: s.maxAge})
	}
	httpapi.SetActor(c, httpapi.Actor{UserID: sess.UserID, SessionID: sess.ID})
	return true, nil
}

// Require answers 401 not_authenticated unless the request carries a valid session.
func (s *Sessions) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, err := s.authenticate(c)
		if err != nil {
			httpapi.Fail(c, err)
			return
		}
		if !ok {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		c.Next()
	}
}

// Optional records the actor when the request carries a valid session and lets every request through.
func (s *Sessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := s.authenticate(c); err != nil {
			httpapi.Fail(c, err)
			return
		}
		c.Next()
	}
}

var _ httpapi.SessionAuth = (*Sessions)(nil)
