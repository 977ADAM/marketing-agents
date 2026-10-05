package campaignservice_test

import (
	"context"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
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
