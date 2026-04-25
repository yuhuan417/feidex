package app

import "feidex/internal/app/backendcaps"

type backendFeature = backendcaps.Feature
type backendCapabilitySpec = backendcaps.CapabilitySpec
type conversationPresentation = backendcaps.ConversationPresentation

const backendFeatureReview = backendcaps.FeatureReview
const backendFeatureSkills = backendcaps.FeatureSkills
const backendFeatureFastMode = backendcaps.FeatureFastMode
const backendFeatureConversationThreadCommands = backendcaps.FeatureConversationThreadCommands
const backendFeatureConversationSessionCommands = backendcaps.FeatureConversationSessionCommands
const backendFeatureConversationPermissions = backendcaps.FeatureConversationPermissions
const backendFeatureWorkspacePermissions = backendcaps.FeatureWorkspacePermissions

func backendCapabilityForKind(kind string) backendcaps.CapabilitySpec {
	return backendcaps.ForKind(kind)
}

func backendCapabilityKinds() []string {
	return backendcaps.Kinds()
}

func backendConversationHelpEntries(backend string, specs []helpCommandSpec) []helpCommandSpec {
	return backendcaps.ConversationHelpEntries(backend, specs)
}
