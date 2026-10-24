package integration

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/slack-go/slack"
)

// slackScope is what the Slack integration asks for, fixed by the Slack app registration.
const slackScope = "channels:read,chat:write,chat:write.public,chat:write.customize,groups:read"

// slackAuthorizeURL is forma2's SLACK_AUTH_URL with the state.
func (p *providers) slackAuthorizeURL(state string) string {
	q := []string{
		"client_id=" + url.QueryEscape(p.cfg.SlackClientID),
		"scope=" + url.QueryEscape(slackScope),
		"redirect_uri=" + url.QueryEscape(p.slackRedirectURI()),
		"state=" + url.QueryEscape(state),
	}
	return p.slack + "/oauth/v2/authorize?" + strings.Join(q, "&")
}

func (p *providers) slackAPI() string { return p.slack + "/api/" }

func (p *providers) slackClient(token string) *slack.Client {
	return slack.New(token, slack.OptionAPIURL(p.slackAPI()), slack.OptionHTTPClient(p.http))
}

// slackRefusal is Slack answering ok: false, with its error code.
type slackRefusal struct{ code string }

func (e *slackRefusal) Error() string { return e.code }

// slackError turns slack-go's errors into forma2's: Slack's own error code when it answered ok: false, "Network response was not ok" for an HTTP failure status.
func slackError(err error) error {
	var refused slack.SlackErrorResponse
	if errors.As(err, &refused) {
		return &slackRefusal{code: refused.Err}
	}
	var status slack.StatusCodeError
	if errors.As(err, &status) {
		return errors.New("Network response was not ok") //nolint:staticcheck // forma2's message
	}
	return err
}

// slackExchange is forma2's Slack callback token exchange (oauth.v2.access), kept as forma2 stored it. Slack refusing the code is a *slackRefusal.
func (p *providers) slackExchange(ctx context.Context, code string) (*SlackKey, error) {
	res, err := slack.GetOAuthV2ResponseContext(ctx, p.http, p.cfg.SlackClientID, p.cfg.SlackClientSecret, code, p.slackRedirectURI(), slack.OAuthOptionAPIURL(p.slackAPI()))
	if err != nil {
		return nil, slackError(err)
	}
	k := &SlackKey{AppID: res.AppID, TokenType: res.TokenType, AccessToken: res.AccessToken, BotUserID: res.BotUserID}
	k.AuthedUser.ID = res.AuthedUser.ID
	k.Team.ID, k.Team.Name = res.Team.ID, res.Team.Name
	return k, nil
}

// slackChannels is forma2's fetchChannels: every public and private channel the bot is in, 200 a page.
func (p *providers) slackChannels(ctx context.Context, token string) ([]Item, error) {
	client := p.slackClient(token)
	out := []Item{}
	cursor := ""
	for {
		channels, next, err := client.GetConversationsForUserContext(ctx, &slack.GetConversationsForUserParameters{
			Cursor: cursor, Limit: 200, Types: []string{"private_channel", "public_channel"},
		})
		if err != nil {
			return nil, slackError(err)
		}
		for _, c := range channels {
			out = append(out, Item{ID: c.ID, Name: c.Name})
		}
		if next == "" {
			return out, nil
		}
		cursor = next
	}
}

// writeSlack is forma2's writeDataToSlack: one message to the channel, the survey's name over a divider, then each label in bold over its value, the value truncated to Slack's section limit. Nothing is escaped, as forma2 escaped nothing.
func (p *providers) writeSlack(ctx context.Context, token, channelID, surveyName string, values, labels []string) error {
	blocks := []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, surveyName+"\n", false, false), nil, nil),
		slack.NewDividerBlock(),
	}
	for i, v := range values {
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		blocks = append(blocks,
			slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, "*"+label+"*", false, false), nil, nil),
			slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, truncate(v, slackMessageLimit)+"\n", false, false), nil, nil),
		)
	}
	if _, _, err := p.slackClient(token).PostMessageContext(ctx, channelID, slack.MsgOptionBlocks(blocks...)); err != nil {
		return fmt.Errorf("slack chat.postMessage: %w", slackError(err))
	}
	return nil
}
