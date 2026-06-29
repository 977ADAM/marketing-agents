package orchestrator

import "testing"

func TestComputePercent(t *testing.T) {
	cases := []struct {
		ph          Phase
		done, total int
		want        int
	}{
		{PhaseStrategizing, 0, 0, 5},
		{PhaseProducing, 0, 2, 10},
		{PhaseProducing, 1, 2, 52}, // 10 + 85*1/2 = 52 (округление вниз)
		{PhaseProducing, 2, 2, 95},
		{PhaseDone, 2, 2, 100},
		{PhaseProducing, 0, 0, 10},  // total==0 guard returns pctPlanned
		{PhaseFailed, 3, 5, 0},      // failed path returns 0 (caller ignores it)
	}
	for _, c := range cases {
		if got := computePercent(c.ph, c.done, c.total); got != c.want {
			t.Errorf("computePercent(%q,%d,%d) = %d, want %d", c.ph, c.done, c.total, got, c.want)
		}
	}
}

func TestNopProgressDoesNotPanic(t *testing.T) {
	var p Progress = NopProgress{}
	p.Strategizing()
	p.TopicsPlanned([]string{"a"})
	p.TopicWriting(0)
	p.TopicReviewing(0, 1)
	p.TopicRevising(0, 1)
	p.TopicDone(0, 90)
}
