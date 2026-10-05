package reviewservice_test

import (
	"context"
	domain "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/review/service"
	mock "github.com/977ADAM/marketing-agents/internal/testkit/mock"
	"testing"
)

func TestCheckersRejectInvalidScore(t *testing.T) {
	for _, role := range []string{"compliance", "quality"} {
		for _, body := range []string{`{"issues":[]}`, `{"score":null,"issues":[]}`, `{"score":101,"issues":[]}`, `{"score":-1,"issues":[]}`} {
			t.Run(role+body, func(t *testing.T) {
				f := mock.NewLLM()
				f.Responses[role] = []string{body}
				var err error
				if role == "compliance" {
					_, _, err = service.NewComplianceChecker(f).Run(context.Background(), "brief", domain.TextToReview{Body: "B"})
				} else {
					_, _, err = service.NewQualityChecker(f).Run(context.Background(), domain.TextToReview{Body: "B"})
				}
				if err == nil {
					t.Fatal("invalid score accepted")
				}
			})
		}
	}
}
func TestZeroScoreIsValid(t *testing.T) {
	f := mock.NewLLM()
	f.Responses["quality"] = []string{`{"score":0,"issues":["bad"]}`}
	s, _, err := service.NewQualityChecker(f).Run(context.Background(), domain.TextToReview{Body: "B"})
	if err != nil || s.Score != 0 {
		t.Fatalf("score: %+v, %v", s, err)
	}
}
