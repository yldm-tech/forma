// Package mailertest provides a mailer.Transport that keeps messages in memory, for tests that assert what was sent.
package mailertest

import (
	"context"
	"sync"

	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
)

// Transport records every message it is given. Set Err to make deliveries fail.
type Transport struct {
	mu       sync.Mutex
	messages []mailer.Message
	Err      error
}

func (t *Transport) Send(_ context.Context, msg mailer.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Err != nil {
		return t.Err
	}
	t.messages = append(t.messages, msg)
	return nil
}

// Messages returns a copy of what was delivered, in order.
func (t *Transport) Messages() []mailer.Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]mailer.Message(nil), t.messages...)
}
