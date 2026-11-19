package buildinfo

import (
	"runtime"
	"runtime/debug"
)

var Version = "dev"

type Info struct {
	Version    string `json:"version"`
	Revision   string `json:"revision"`
	Modified   bool   `json:"modified"`
	GoVersion  string `json:"goVersion"`
	Platform   string `json:"platform"`
	UIEmbedded bool   `json:"uiEmbedded"`
}

func Current() Info {
	info := Info{
		Version:   Version,
		Revision:  "unknown",
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.Revision = setting.Value
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
	}
	return info
}
