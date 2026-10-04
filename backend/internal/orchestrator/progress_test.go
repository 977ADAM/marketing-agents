package orchestrator_test

import (
	"testing"

	"github.com/977ADAM/marketing-agents/internal/orchestrator"
)

func TestComputePercent(t *testing.T) {
	cases := []struct {
		ph          orchestrator.Phase
		done, total int
		want        int
	}{
		{orchestrator.PhaseStrategizing, 0, 0, 10}, // позиционирование идёт после подбора тем
		{orchestrator.PhaseResearching, 0, 0, 5},   // сеялок ещё нет
		{orchestrator.PhaseResearching, 1, 2, 7},   // 5 + 5*1/2 — сбор спроса идёт
		{orchestrator.PhaseResearching, 2, 2, 10},  // сбор спроса закончен
		{orchestrator.PhaseProducing, 0, 2, 10},
		{orchestrator.PhaseProducing, 1, 2, 52}, // 10 + 85*1/2 = 52 (округление вниз)
		{orchestrator.PhaseProducing, 2, 2, 95},
		{orchestrator.PhaseDone, 2, 2, 100},
		{orchestrator.PhaseProducing, 0, 0, 10}, // total==0 guard returns pctPlanned
		{orchestrator.PhaseFailed, 3, 5, 0},     // failed path returns 0 (caller ignores it)
	}
	for _, c := range cases {
		if got := orchestrator.ComputePercent(c.ph, c.done, c.total); got != c.want {
			t.Errorf("orchestrator.ComputePercent(%q,%d,%d) = %d, want %d", c.ph, c.done, c.total, got, c.want)
		}
	}
}

func TestNopProgressDoesNotPanic(t *testing.T) {
	var p orchestrator.Progress = orchestrator.NopProgress{}
	p.Strategizing()
	p.TopicsPlanned([]string{"a"})
	p.TopicWriting(0)
	p.TopicReviewing(0, 1)
	p.TopicRevising(0, 1)
	p.TopicDone(0, 90)

	// Заглушка реализует и необязательную часть прогресса — подбор тем.
	rp, ok := p.(orchestrator.ResearchProgress)
	if !ok {
		t.Fatal("NopProgress должен реализовывать ResearchProgress")
	}
	rp.Researching(orchestrator.StageSeeds)
	rp.ResearchSeeds([]string{"a"})
	rp.ResearchSeedDone(0)
}
