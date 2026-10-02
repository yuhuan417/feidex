package review

type TargetSpec struct {
	Type         string
	Branch       string
	CommitSHA    string
	CommitTitle  string
	Instructions string
}

const (
	TargetUncommitted = "uncommittedChanges"
	TargetBaseBranch  = "baseBranch"
	TargetCommit      = "commit"
	TargetCustom      = "custom"
)
