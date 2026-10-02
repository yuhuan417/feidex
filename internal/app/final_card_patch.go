package app

import "feidex/internal/adapter/feishu/finalcardpatch"

type finalCardPatchTracker = finalcardpatch.Tracker

func newFinalCardPatchTracker() *finalCardPatchTracker { return finalcardpatch.NewTracker() }
