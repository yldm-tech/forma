// Package analytics is forma2's server-side product analytics (apps/web/lib/posthog): events, person and group properties sent to PostHog's EU cloud when POSTHOG_KEY is set, and nothing at all otherwise. Like forma2's module-level client, one process-wide client is installed at startup, so a feature module captures an event without carrying a dependency through its constructors. Capturing never fails the caller: the client queues the event and sends it in the background, and a failure is logged.
package analytics

import (
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Host is forma2's PostHog host, its EU cloud.
const Host = "https://eu.i.posthog.com"

// Event properties forma2 added to every server event, so its dashboards can tell server events from the browser's.
const (
	library = "posthog-node"
	source  = "server"
)

// Properties are an event's, a person's or a group's properties.
type Properties map[string]any

// Groups are the PostHog groups an event belongs to; an empty id leaves its group out.
type Groups struct {
	OrganizationID string
	WorkspaceID    string
}

// Group types, forma2's PostHogGroupType.
const (
	GroupOrganization = "organization"
	GroupWorkspace    = "workspace"
)

// Client sends to one PostHog project.
type Client struct {
	ph  posthog.Client
	log *slog.Logger
}

// current is the process-wide client; nil while PostHog is not configured.
var current atomic.Pointer[Client]

// Setup installs the process-wide client when POSTHOG_KEY is set. The returned function sends what is still queued and closes the client; call it before the process exits.
func Setup(cfg config.Config, log *slog.Logger) (func(), error) {
	if cfg.Telemetry.PostHogKey == "" {
		return func() {}, nil
	}
	c, err := NewClient(cfg.Telemetry.PostHogKey, Host, log)
	if err != nil {
		return nil, err
	}
	restore := Install(c)
	return func() {
		restore()
		c.Close()
	}, nil
}

// NewClient is a client of the project key at endpoint. Events are sent one at a time as they are captured, forma2's flushAt 1: these are low-volume product events, and delivering each beats batching them.
func NewClient(key, endpoint string, log *slog.Logger) (*Client, error) {
	ph, err := posthog.NewWithConfig(key, posthog.Config{
		Endpoint:  endpoint,
		BatchSize: 1,
		Interval:  time.Second,
		Logger:    slogLogger{log},
	})
	if err != nil {
		return nil, fmt.Errorf("create PostHog client: %w", err)
	}
	return &Client{ph: ph, log: log}, nil
}

// Install makes c the process-wide client and returns the function that puts the previous one back.
func Install(c *Client) func() {
	previous := current.Swap(c)
	return func() { current.CompareAndSwap(c, previous) }
}

// Close sends what is queued and stops the client.
func (c *Client) Close() {
	if err := c.ph.Close(); err != nil {
		c.log.Error("Error shutting down PostHog server client", "err", err)
	}
}

// Enabled reports whether events are sent anywhere.
func Enabled() bool { return current.Load() != nil }

// Capture is forma2's capturePostHogEvent: event for distinctID with props and groups.
func Capture(distinctID, event string, props Properties, groups Groups) {
	c := current.Load()
	if c == nil {
		return
	}
	properties := posthog.NewProperties()
	for k, v := range props {
		properties.Set(k, v)
	}
	properties.Set("source", source)
	msg := posthog.Capture{DistinctId: distinctID, Event: event, Properties: properties, Library: library}
	if g := groups.posthog(); g != nil {
		msg.Groups = g
	}
	if err := c.ph.Enqueue(msg); err != nil {
		c.log.Warn("Failed to capture PostHog event", "eventName", event, "err", err)
	}
}

// Identify is forma2's identifyPostHogPerson.
func Identify(distinctID string, props Properties) {
	c := current.Load()
	if c == nil {
		return
	}
	if err := c.ph.Enqueue(posthog.Identify{DistinctId: distinctID, Properties: posthog.Properties(props)}); err != nil {
		c.log.Warn("Failed to identify PostHog person", "err", err)
	}
}

// GroupIdentify is forma2's groupIdentifyPostHog: properties merged into a group, organization or workspace.
func GroupIdentify(groupType, key string, props Properties) {
	c := current.Load()
	if c == nil {
		return
	}
	if err := c.ph.Enqueue(posthog.GroupIdentify{Type: groupType, Key: key, Properties: posthog.Properties(props)}); err != nil {
		c.log.Warn("Failed to identify PostHog group", "groupType", groupType, "groupKey", key, "err", err)
	}
}

// EmailDomain is forma2's getEmailDomain: the lowercased domain of an email address, empty without one.
func EmailDomain(email string) string {
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return ""
	}
	return strings.ToLower(domain)
}

func (g Groups) posthog() posthog.Groups {
	if g.OrganizationID == "" && g.WorkspaceID == "" {
		return nil
	}
	out := posthog.NewGroups()
	if g.OrganizationID != "" {
		out.Set(GroupOrganization, g.OrganizationID)
	}
	if g.WorkspaceID != "" {
		out.Set(GroupWorkspace, g.WorkspaceID)
	}
	return out
}

// slogLogger routes the client's own diagnostics to the process log; its debug output is dropped.
type slogLogger struct{ log *slog.Logger }

func (l slogLogger) Debugf(string, ...any) {}

func (l slogLogger) Logf(format string, args ...any) { l.log.Info(fmt.Sprintf(format, args...)) }

func (l slogLogger) Warnf(format string, args ...any) { l.log.Warn(fmt.Sprintf(format, args...)) }

func (l slogLogger) Errorf(format string, args ...any) {
	l.log.Error(fmt.Sprintf(format, args...))
}

// OrganizationCreated is forma2's events for a new organization with its first workspace, the same on every path that creates one (setup, the organization switcher, sign-up): the two groups' names and the creator's email domain, organization_created and workspace_created.
func OrganizationCreated(userID, organizationID, organizationName, workspaceID, workspaceName, emailDomain string, isFirstOrganization bool) {
	GroupIdentify(GroupOrganization, organizationID, Properties{"name": organizationName, "email_domain": emailDomain})
	GroupIdentify(GroupWorkspace, workspaceID, Properties{"name": workspaceName})
	groups := Groups{OrganizationID: organizationID, WorkspaceID: workspaceID}
	Capture(userID, "organization_created", Properties{"organization_id": organizationID, "is_first_org": isFirstOrganization}, groups)
	Capture(userID, "workspace_created", Properties{"organization_id": organizationID, "workspace_id": workspaceID, "name": workspaceName}, groups)
}

// WorkspaceCreated is forma2's events for a workspace added to an existing organization.
func WorkspaceCreated(userID, organizationID, workspaceID, workspaceName string) {
	GroupIdentify(GroupWorkspace, workspaceID, Properties{"name": workspaceName})
	Capture(userID, "workspace_created", Properties{"organization_id": organizationID, "workspace_id": workspaceID, "name": workspaceName}, Groups{OrganizationID: organizationID, WorkspaceID: workspaceID})
}

// FeatureFlag is forma2's getPostHogFeatureFlag: the flag's value for distinctID within groups, false without PostHog or when PostHog cannot answer.
func FeatureFlag(distinctID, key string, groups Groups) any {
	c := current.Load()
	if c == nil {
		return false
	}
	payload := posthog.FeatureFlagPayload{Key: key, DistinctId: distinctID}
	if g := groups.posthog(); g != nil {
		payload.Groups = g
	}
	value, err := c.ph.GetFeatureFlag(payload)
	if err != nil {
		c.log.Warn("Failed to evaluate PostHog feature flag", "flagKey", key, "err", err)
		return false
	}
	if value == nil {
		return false
	}
	return value
}
