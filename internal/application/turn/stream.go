package turn

type StreamSummary struct {
	SawFinal                bool
	SawPlanItem             bool
	PlanCompleted           bool
	PlanMarkdown            string
	PlanMessageID           string
	ShouldUsePlanExitPrompt bool
	LastError               string
	WorkingMessageID        string
	FinalText               string
	FinalReuseMessageID     string
}
