package v3api

import (
	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Auditor records the audit events of v3 mutations.
type Auditor struct {
	emitter *audit.Emitter
	cfg     config.Config
}

func NewAuditor(cfg config.Config, emitter *audit.Emitter) *Auditor {
	return &Auditor{emitter: emitter, cfg: cfg}
}

// Record is forma2's buildV3AuditLog plus queueV3AuditLog: the principal as the actor (a user, or the API key as "api" in its own organization), the absolute URL of the call, and success only when err is nil. A failure carries the request id as its event id and none of the objects. Fields of e left empty are recorded as unknown, as forma2 did for a request refused before the target was known.
func (a *Auditor) Record(c *gin.Context, e audit.Event, err error) {
	ctx := c.Request.Context()
	if p := PrincipalFrom(ctx); p != nil {
		if p.APIKey != nil {
			e.ActorType, e.ActorID = audit.ActorAPI, p.APIKey.APIKeyID.String()
			if e.OrganizationID == "" {
				e.OrganizationID = p.APIKey.OrganizationID.String()
			}
		} else {
			e.ActorType, e.ActorID = audit.ActorUser, p.UserID.String()
		}
	}
	e.APIURL = a.cfg.WebAppURL.Scheme + "://" + a.cfg.WebAppURL.Host + c.Request.URL.RequestURI()
	e.Status = audit.StatusSuccess
	if err != nil {
		e.Status, e.EventID, e.OldObject, e.NewObject = audit.StatusFailure, RequestID(c), nil, nil
	}
	a.emitter.Log(ctx, audit.FromRequest(ctx, e))
}
