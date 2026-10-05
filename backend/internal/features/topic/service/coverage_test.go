package topicservice

import (
	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	"testing"
)

func TestPhraseCoverage(t *testing.T) {
	demands := []topic.Demand{
		{Phrase: "seed1", Requests: []topic.PhraseCount{{Phrase: "r1", Count: 100}, {Phrase: "r2", Count: 99}}, Associations: []topic.PhraseCount{{Phrase: "a1", Count: 1000}}},
		{Phrase: "seed2", Requests: []topic.PhraseCount{{Phrase: "r3", Count: 1}}, Associations: []topic.PhraseCount{{Phrase: "a2", Count: 2000}}},
	}
	got := phrasesWithCoverage(demands, 2)
	if len(got) != 2 || got[0].Phrase != "r1" || got[1].Phrase != "r3" || got[1].Seed != "seed2" {
		t.Fatalf("coverage: %+v", got)
	}
	got = phrasesWithCoverage(demands, 4)
	if len(got) != 4 || got[3].Origin != "association" || got[2].Origin != "request" {
		t.Fatalf("origins: %+v", got)
	}
}
