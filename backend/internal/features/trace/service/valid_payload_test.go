package traceservice_test

import (
	"encoding/json"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/trace/service"
	"strings"
	"testing"
)

func TestTruncatedPayloadRemainsValidJSON(t *testing.T) {
	sink := &fakeSink{}
	rec := service.New(sink, trace.Config{Mode: trace.ModeFull, MaxPayloadBytes: 40})
	rec.Event(runCtx("valid"), trace.Event{Payload: map[string]string{"body": strings.Repeat("ш", 100)}})
	data := sink.all()[0].PayloadJSON
	var envelope struct {
		Data      any  `json:"data"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err != nil || !envelope.Truncated || len(data) > 40 {
		t.Fatalf("invalid bounded payload %q: %v", data, err)
	}
}

func TestFinishReleasesLocalSequence(t *testing.T) {
	sink := &fakeSink{}
	rec := service.New(sink, trace.Config{Mode: trace.ModeSummary})
	rec.Event(runCtx("finished"), trace.Event{Name: "run"})
	if service.ActiveRuns(rec) != 1 {
		t.Fatal("local state missing")
	}
	trace.FinishRun(rec, "finished")
	if service.ActiveRuns(rec) != 0 {
		t.Fatal("local state retained")
	}
}
