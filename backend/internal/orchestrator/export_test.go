package orchestrator

import (
	"github.com/977ADAM/marketing-agents/internal/topic"
)

// RankVolume — шов для тестов ранжирования тем: у сезонной темы в межсезонье
// сравнение идёт по пику, а не по текущему объёму.
func RankVolume(c topic.TopicCandidate) int64 { return rankVolume(c) }

// Причины отклонения темы: тесты сверяют их в кандидатах отбора.
const (
	RejectTechnical = rejectTechnical
	RejectLowVolume = rejectLowVolume
)
