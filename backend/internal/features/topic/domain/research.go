package topic

type ResearchRequest struct {
	Briefing    Briefing
	Region      string
	TopicsCount int
}
type SelectedTopic struct {
	Title  string
	Angle  string
	Points []string
}
type ResearchResult struct {
	Topics          []SelectedTopic
	TopicCandidates []TopicCandidate
	WordstatCalls   int
}
