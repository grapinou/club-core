// Package mailer delivers small text messages. Never log Message or SMTP credentials.
package mailer

import (
	"context"
	"errors"
)

type Message struct{ From, To, Subject, Text string }
type Mailer interface {
	Send(context.Context, Message) error
}

// Disabled explicitly reports non-delivery, never pretends a message was sent.
type Disabled struct{}

var ErrDisabled = errors.New("email transport is disabled")

func (Disabled) Send(context.Context, Message) error { return ErrDisabled }
