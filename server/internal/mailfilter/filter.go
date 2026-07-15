// Package mailfilter implements small mailbox delivery rules.
package mailfilter

import (
	"fmt"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/domain"
)

// MessageFields are the values available to filter matching.
type MessageFields struct {
	From    string
	Subject string
	To      string
}

// Match reports whether a rule matches the message. Matching is case-insensitive.
func Match(rule domain.MailFilter, msg MessageFields) bool {
	if !rule.Enabled {
		return false
	}
	var value string
	switch rule.MatchField {
	case "from":
		value = msg.From
	case "subject":
		value = msg.Subject
	case "to":
		value = msg.To
	default:
		return false
	}
	value = strings.ToLower(value)
	want := strings.ToLower(rule.MatchValue)
	switch rule.MatchOp {
	case "contains":
		return strings.Contains(value, want)
	case "equals":
		return value == want
	default:
		return false
	}
}

// Validate checks that a rule belongs to the supported sieve-lite subset.
func Validate(rule domain.MailFilter) error {
	switch rule.MatchField {
	case "from", "subject", "to":
	default:
		return fmt.Errorf("matchField must be from, subject, or to")
	}
	switch rule.MatchOp {
	case "contains", "equals":
	default:
		return fmt.Errorf("matchOp must be contains or equals")
	}
	if strings.TrimSpace(rule.MatchValue) == "" {
		return fmt.Errorf("matchValue is required")
	}
	switch rule.Action {
	case "fileinto":
		arg := strings.TrimSpace(rule.ActionArg)
		if arg == "" || arg == "." || arg == ".." || strings.ContainsAny(arg, `/\`) {
			return fmt.Errorf("fileinto requires a simple folder name")
		}
	case "reject", "flag_spam":
	default:
		return fmt.Errorf("action must be fileinto, reject, or flag_spam")
	}
	return nil
}

// FirstMatch returns the first enabled matching rule (rules are priority ordered).
func FirstMatch(rules []domain.MailFilter, msg MessageFields) *domain.MailFilter {
	for i := range rules {
		if Match(rules[i], msg) {
			return &rules[i]
		}
	}
	return nil
}
