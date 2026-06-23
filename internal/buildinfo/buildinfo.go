package buildinfo

import "strings"

var Version = "0.1.0"

func CurrentVersion() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		return "dev"
	}
	return version
}
