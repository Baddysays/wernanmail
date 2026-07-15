package mailfilter

import (
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/domain"
)

func TestMatch(t *testing.T) {
	msg := MessageFields{From: "Alerts@Example.COM", Subject: "Build FAILED", To: "ops@example.net"}
	tests := []struct {
		name string
		rule domain.MailFilter
		want bool
	}{
		{"subject contains", domain.MailFilter{Enabled: true, MatchField: "subject", MatchOp: "contains", MatchValue: "failed"}, true},
		{"from equals case insensitive", domain.MailFilter{Enabled: true, MatchField: "from", MatchOp: "equals", MatchValue: "alerts@example.com"}, true},
		{"to differs", domain.MailFilter{Enabled: true, MatchField: "to", MatchOp: "equals", MatchValue: "other@example.net"}, false},
		{"disabled", domain.MailFilter{Enabled: false, MatchField: "subject", MatchOp: "contains", MatchValue: "failed"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Match(tt.rule, msg); got != tt.want {
				t.Fatalf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFirstMatchUsesPriorityOrder(t *testing.T) {
	rules := []domain.MailFilter{
		{ID: 1, Enabled: true, MatchField: "subject", MatchOp: "contains", MatchValue: "invoice", Action: "fileinto", ActionArg: "Finance"},
		{ID: 2, Enabled: true, MatchField: "from", MatchOp: "contains", MatchValue: "example", Action: "reject"},
	}
	got := FirstMatch(rules, MessageFields{From: "sender@example.com", Subject: "Invoice 123"})
	if got == nil || got.ID != 1 {
		t.Fatalf("unexpected first match: %+v", got)
	}
}

func TestValidateRejectsUnsafeFolder(t *testing.T) {
	rule := domain.MailFilter{
		Enabled: true, MatchField: "subject", MatchOp: "contains", MatchValue: "x",
		Action: "fileinto", ActionArg: "../Trash",
	}
	if err := Validate(rule); err == nil {
		t.Fatal("expected unsafe folder validation error")
	}
}
