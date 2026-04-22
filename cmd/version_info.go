package cmd

import (
	"fmt"
	"runtime/debug"
	"strings"
)

var readBuildInfo = debug.ReadBuildInfo

func currentVersion() string {
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return "unknown"
	}

	version := strings.TrimSpace(info.Main.Version)
	if version != "" && version != "(devel)" {
		return version
	}

	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	revision := settings["vcs.revision"]
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if revision == "" {
		return "devel"
	}
	if settings["vcs.modified"] == "true" {
		return fmt.Sprintf("devel (%s, dirty)", revision)
	}
	return fmt.Sprintf("devel (%s)", revision)
}
