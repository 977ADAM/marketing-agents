package run_test

import (
	"testing"

	"github.com/977ADAM/marketing-agents/internal/run"
)

func TestComputePercent(t *testing.T) {
	cases := []struct {
		ph          run.Phase
		done, total int
		want        int
	}{
		{run.PhaseStrategizing, 0, 0, 10}, // позиционирование идёт после подбора тем
		{run.PhaseResearching, 0, 0, 5},   // сеялок ещё нет
		{run.PhaseResearching, 1, 2, 7},   // 5 + 5*1/2 — сбор спроса идёт
		{run.PhaseResearching, 2, 2, 10},  // сбор спроса закончен
		{run.PhaseProducing, 0, 2, 10},
		{run.PhaseProducing, 1, 2, 52}, // 10 + 85*1/2 = 52 (округление вниз)
		{run.PhaseProducing, 2, 2, 95},
		{run.PhaseDone, 2, 2, 100},
		{run.PhaseProducing, 0, 0, 10}, // total==0 guard returns pctPlanned
		{run.PhaseFailed, 3, 5, 0},     // failed path returns 0 (caller ignores it)
	}
	for _, c := range cases {
		if got := run.ComputePercent(c.ph, c.done, c.total); got != c.want {
			t.Errorf("run.ComputePercent(%q,%d,%d) = %d, want %d", c.ph, c.done, c.total, got, c.want)
		}
	}
}

func TestNopProgressDoesNotPanic(t *testing.T) {
	var p run.Progress = run.NopProgress{}
	p.Strategizing()
	p.TopicsPlanned([]string{"a"})
	p.TopicWriting(0)
	p.TopicReviewing(0, 1)
	p.TopicRevising(0, 1)
	p.TopicDone(0, 90)

	// Заглушка реализует и необязательную часть прогресса — подбор тем.
	rp, ok := p.(run.ResearchProgress)
	if !ok {
		t.Fatal("NopProgress должен реализовывать ResearchProgress")
	}
	rp.Researching(run.StageSeeds)
	rp.ResearchSeeds([]string{"a"})
	rp.ResearchSeedDone(0)
}
