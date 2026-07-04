package nats

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

// Publisher wraps a NATS connection for publishing domain events.
type Publisher struct {
	conn *nats.Conn
}

// NewPublisher connects to NATS and returns a Publisher.
func NewPublisher(url string) (*Publisher, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return &Publisher{conn: nc}, nil
}

// Publish sends a message to the given subject.
func (p *Publisher) Publish(subject string, data []byte) error {
	if p == nil || p.conn == nil {
		return nil
	}
	return p.conn.Publish(subject, data)
}

// Close closes the NATS connection.
func (p *Publisher) Close() {
	if p != nil && p.conn != nil {
		p.conn.Close()
	}
}
