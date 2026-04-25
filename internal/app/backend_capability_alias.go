package app

import "feidex/internal/app/backendcaps"

type backendFeature = backendcaps.Feature
type backendCapabilitySpec = backendcaps.CapabilitySpec

const backendFeatureReview = backendcaps.FeatureReview
const backendFeatureSkills = backendcaps.FeatureSkills
const backendFeatureFastMode = backendcaps.FeatureFastMode
const backendFeatureConversationThreadCommands = backendcaps.FeatureConversationThreadCommands
const backendFeatureConversationSessionCommands = backendcaps.FeatureConversationSessionCommands

func backendCapabilityForKind(kind string) backendcaps.CapabilitySpec {
	return backendcaps.ForKind(kind)
}

func backendConversationHelpEntries(backend string, specs []helpCommandSpec) []helpCommandSpec {
	return backendcaps.ConversationHelpEntries(backend, specs)
}
