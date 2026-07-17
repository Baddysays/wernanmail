package alerts

import (
	"testing"
	"time"
)

func TestCollectIssues(t *testing.T) {
	issues := CollectIssues(Snapshot{
		StackMode:    "proc",
		StackMissing: []string{"worker"},
		QueuePending: 80,
		QueueDead:    2,
	}, 50)
	keys := map[string]bool{}
	for _, i := range issues {
		keys[i.Key] = true
	}
	for _, want := range []string{"queue.dead", "queue.pending", "stack.missing"} {
		if !keys[want] {
			t.Fatalf("missing issue %s in %#v", want, issues)
		}
	}
}

func TestCollectIssuesHealthy(t *testing.T) {
	issues := CollectIssues(Snapshot{StackMode: "proc", QueuePending: 3}, 50)
	if len(issues) != 0 {
		t.Fatalf("want none, got %#v", issues)
	}
}

func TestSplitList(t *testing.T) {
	got := splitList("Ops@Example.com, bad,, other@x.test")
	if len(got) != 2 || got[0] != "ops@example.com" {
		t.Fatalf("got %#v", got)
	}
}

func TestConfigHasChannel(t *testing.T) {
	if (Config{}).HasChannel() {
		t.Fatal("empty should have no channel")
	}
	if !(Config{Emails: []string{"a@b.c"}}).HasChannel() {
		t.Fatal("email channel")
	}
	if !(Config{TelegramToken: "t", TelegramChatID: "1"}).HasChannel() {
		t.Fatal("telegram channel")
	}
}

func TestCooldown(t *testing.T) {
	w := NewWatcher()
	now := w.lastSent // ensure map exists
	_ = now
	if !w.allow("queue.dead", timeMinute(), mustParseTime("2026-01-01T00:00:00Z")) {
		t.Fatal("first allow")
	}
	w.mark("queue.dead", mustParseTime("2026-01-01T00:00:00Z"))
	if w.allow("queue.dead", timeMinute(), mustParseTime("2026-01-01T00:00:30Z")) {
		t.Fatal("should throttle")
	}
	if !w.allow("queue.dead", timeMinute(), mustParseTime("2026-01-01T00:02:00Z")) {
		t.Fatal("should allow after cooldown")
	}
}

func timeMinute() time.Duration { return time.Minute }

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
