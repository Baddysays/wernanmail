package queue

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Service wraps QueueStore with helpers.
type Service struct {
	Store store.QueueStore
}

func (s *Service) EnqueueJSON(ctx context.Context, kind domain.QueueJobKind, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.Store.Enqueue(ctx, &domain.QueueJob{
		Kind:        kind,
		PayloadJSON: string(b),
		MaxAttempts: 8,
		NextAt:      time.Now().UTC(),
	})
}

// Backoff returns next retry time with exponential backoff + jitter.
func Backoff(attempts int) time.Time {
	base := math.Pow(2, float64(min(attempts, 8)))
	sec := base*5 + rand.Float64()*3
	if sec > 3600 {
		sec = 3600
	}
	return time.Now().UTC().Add(time.Duration(sec) * time.Second)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// InboundPayload is queued after pipeline accept.
type InboundPayload struct {
	MailboxID int64   `json:"mailboxId"`
	Folder    string  `json:"folder"`
	RawB64    string  `json:"rawB64"`
	Subject   string  `json:"subject"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	SpamScore float64 `json:"spamScore"`
}

// OutboundPayload is a message to send to the internet.
type OutboundPayload struct {
	From     string   `json:"from"`
	To       []string `json:"to"`
	RawB64   string   `json:"rawB64"`
	DomainID int64    `json:"domainId"`
}
