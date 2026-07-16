package metrics_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/metrics"
)

func TestWritePrometheus(t *testing.T) {
	r := metrics.New("worker")
	r.Inc("jobs_ok", 2)
	r.Set("queue_pending", 3)
	var buf bytes.Buffer
	r.WritePrometheus(&buf)
	out := buf.String()
	for _, want := range []string{
		`wernanmail_up{process="worker"} 1`,
		`wernanmail_jobs_ok_total{process="worker"} 2`,
		`wernanmail_queue_pending{process="worker"} 3`,
		`wernanmail_go_goroutines{process="worker"}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
