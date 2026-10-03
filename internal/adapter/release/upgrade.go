package release

import (
	"context"
	upgradeapp "feidex/internal/application/upgrade"
	"feidex/internal/release"
)

type Client interface {
	LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error)
	LatestDevLinuxBinary(context.Context, string) (*release.ReleaseInfo, error)
	LinuxBinaryByVersion(context.Context, string, string) (*release.ReleaseInfo, error)
}
type Gateway struct{ Client func() Client }

func (g Gateway) Query(ctx context.Context, version, arch string, dev bool) (upgradeapp.Release, error) {
	var info *release.ReleaseInfo
	var err error
	switch {
	case dev:
		info, err = g.Client().LatestDevLinuxBinary(ctx, arch)
	case version != "":
		info, err = g.Client().LinuxBinaryByVersion(ctx, version, arch)
	default:
		info, err = g.Client().LatestLinuxBinary(ctx, arch)
	}
	if err != nil {
		return upgradeapp.Release{}, err
	}
	return upgradeapp.Release{Version: info.Version, ReleaseTag: info.ReleaseTag, SourceCommit: info.SourceCommit, BinaryURL: info.BinaryURL, ExpectedSHA256: info.ExpectedSHA256, HTMLURL: info.HTMLURL, PublishedAt: info.PublishedAt}, nil
}
func (Gateway) Compare(current, target string) (int, error) {
	return release.CompareVersions(current, target)
}
