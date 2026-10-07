package campaignservice_test

import (
	"context"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	traceservice "github.com/977ADAM/marketing-agents/internal/features/trace/service"
	mock "github.com/977ADAM/marketing-agents/internal/testkit/mock"
	"strings"
	"testing"
)

func TestCriticReceivesGoalAndProduct(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["critic"] = []string{`{"score":80,"issues":[],"verdict":"accept"}`}
	_, _, err := service.NewCritic(f).Run(context.Background(), testBrief(), campaign.Strategy{}, campaign.Topic{}, campaign.Article{Title: "T", Body: "B"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := f.LastRequest()
	for _, s := range []string{testBrief().Product, testBrief().Goal} {
		if !strings.Contains(req.User, s) {
			t.Errorf("missing %q in prompt", s)
		}
	}
}
func TestRevisionPreservesBrief(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["copywriter"] = []string{`{"title":"T","body":"B"}`}
	_, _, err := service.NewCopywriter(f).Revise(context.Background(), testBrief(), campaign.Strategy{}, campaign.Topic{}, campaign.Article{Title: "T", Body: "B"}, campaign.Review{Score: 50})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := f.LastRequest()
	if !strings.Contains(req.User, testBrief().Product) {
		t.Fatal("revision lost product")
	}
}
func TestMissingScoreRejected(t *testing.T) {
	for _, response := range []string{`{"issues":[],"verdict":"accept"}`, `{"score":null,"issues":[],"verdict":"accept"}`} {
		f := mock.NewLLM()
		f.Responses["critic"] = []string{response}
		_, _, err := service.NewCritic(f).Run(context.Background(), testBrief(), campaign.Strategy{}, campaign.Topic{}, campaign.Article{})
		if err == nil {
			t.Errorf("accepted %s", response)
		}
	}
}
func TestInvalidVerdictRejected(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["critic"] = []string{`{"score":80,"issues":[],"verdict":"unknown"}`}
	_, _, err := service.NewCritic(f).Run(context.Background(), testBrief(), campaign.Strategy{}, campaign.Topic{}, campaign.Article{})
	if err == nil {
		t.Fatal("accepted invalid verdict")
	}
}
func TestWhitespaceArticleRejected(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["copywriter"] = []string{`{"title":"   ","body":"\n"}`}
	_, _, err := service.NewCopywriter(f).Run(context.Background(), testBrief(), campaign.Strategy{}, campaign.Topic{})
	if err == nil {
		t.Fatal("accepted whitespace")
	}
}

func TestZeroCriticIterationsPreservesArticle(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["strategist"] = []string{`{"positioning":"P","topics":[{"title":"T","angle":"A","points":["P"]}]}`}
	f.Responses["copywriter"] = []string{`{"title":"T","body":"B"}`}
	result, err := service.NewWorkflow(f, service.Options{CriticMaxIter: 0}).Run(context.Background(), testBrief(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deliverables) != 1 || result.Deliverables[0].Body != "B" || result.Deliverables[0].Review != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestTopicsCountWithoutWordstat(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["strategist"] = []string{`{"positioning":"P","topics":[{"title":"T"},{"title":"T2"}]}`}
	f.Responses["copywriter"] = []string{`{"title":"T","body":"B"}`}
	b := testBrief()
	b.TopicsCount = 1
	r, err := service.NewWorkflow(f, service.Options{MaxTopics: 5}).Run(context.Background(), b, nil)
	if err != nil || len(r.Deliverables) != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if !strings.Contains(f.Requests[0].User, "ровно 1") {
		t.Fatal("count absent from strategist prompt")
	}
}
func TestFewerTopicsHasWarning(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["strategist"] = []string{`{"positioning":"P","topics":[{"title":"T"}]}`}
	f.Responses["copywriter"] = []string{`{"title":"T","body":"B"}`}
	b := testBrief()
	b.TopicsCount = 2
	r, err := service.NewWorkflow(f, service.Options{MaxTopics: 5}).Run(context.Background(), b, nil)
	if err != nil || len(r.Strategy.Warnings) == 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestResearchFailurePreservesUsage(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["semanticist_seeds"] = []string{`{"seeds":["seed"]}`}
	source := mock.NewWordstat()
	source.Err = fmt.Errorf("upstream failed")
	res, err := service.NewWorkflow(f, service.Options{Wordstat: source, CostPer1KPrompt: 1}).Run(context.Background(), testBrief(), nil)
	if err == nil || res.Usage.PromptTokens != 10 || res.CostUSD != 0.01 {
		t.Fatalf("usage=%+v cost=%v err=%v", res.Usage, res.CostUSD, err)
	}
}

type cancelClient struct{ cancel context.CancelFunc }

func (c cancelClient) Complete(context.Context, string, string, string, any) (corellm.Usage, error) {
	c.cancel()
	return corellm.Usage{PromptTokens: 10}, context.Canceled
}

type contextSink struct{ records []trace.Record }

func (s *contextSink) SaveRunEvent(ctx context.Context, r trace.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.records = append(s.records, r)
	return nil
}
func TestCancelledRunStillWritesResultTrace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = trace.WithRunID(ctx, "timeout")
	sink := &contextSink{}
	rec := traceservice.New(sink, trace.Config{Mode: trace.ModeSummary})
	_, err := service.NewWorkflow(cancelClient{cancel}, service.Options{Recorder: rec}).Run(ctx, testBrief(), nil)
	if err == nil {
		t.Fatal("ожидалась ошибка отменённого прогона")
	}
	if len(sink.records) == 0 {
		t.Fatal("трасса пуста: итоговая запись провалившегося прогона не сохранена")
	}
	last := sink.records[len(sink.records)-1]
	if last.Kind != trace.KindResult || last.RunID != "timeout" || last.Status != trace.StatusError {
		t.Fatalf("последняя запись не итог провалившегося прогона: %+v", last)
	}
	var failedPhase bool
	for _, rec := range sink.records {
		if rec.Kind == trace.KindPhase && rec.Name == "failed" && rec.Status == trace.StatusError {
			failedPhase = true
		}
	}
	if !failedPhase {
		t.Fatalf("в трассе нет фазы failed: %+v", sink.records)
	}
}
