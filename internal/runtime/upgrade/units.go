package upgrade

import (
	"context"
	upgradeapp "feidex/internal/application/upgrade"
	"feidex/internal/daemon"
)

type Units struct{}

func (Units) Query(ctx context.Context, name string) (*upgradeapp.UnitStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	status, err := daemon.QueryUpgradeUnitStatus(name)
	if err != nil || status == nil {
		return nil, err
	}
	return &upgradeapp.UnitStatus{ActiveState: status.ActiveState, Result: status.Result, JournalTail: status.JournalTail}, nil
}
func (Units) Cleanup(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	daemon.CleanupUpgradeUnit(name)
	return nil
}
