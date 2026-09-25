package version

import (
	_ "embed"
	"strings"
)

var (
	//go:embed VERSION
	rawVersion string
)

func GetVersion() string {
	// The full version is type-{semver} ({semver})
	return rawVersion
}

func GetBuildVersion() string {
	// The build version is the version of the build
	// This is the first semver without the type
	buildVersion, _, _ := strings.Cut(rawVersion, " ")

	return buildVersion[5 : len(buildVersion)-1]
}

func GetChangesVersion() string {
	// The build version is the version of the changes (commits)
	// This is the second semver between the parentheses
	_, buildVersion, _ := strings.Cut(rawVersion, " ")

	return buildVersion[1 : len(buildVersion)-1]
}
