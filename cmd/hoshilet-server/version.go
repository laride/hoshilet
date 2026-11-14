package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Releases may set this with -ldflags "-X main.version=vX.Y.Z".
// Ordinary builds use the version and VCS metadata recorded by the Go toolchain.
var version string

func versionInfo() string {
	v, commit, commitTime, modified := version, "unknown", "unknown", "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				commit = setting.Value
			case "vcs.time":
				commitTime = setting.Value
			case "vcs.modified":
				modified = setting.Value
			}
		}
	}
	if v == "" {
		v = "dev"
	}
	return fmt.Sprintf("hoshilet-server %s\ncommit: %s\ncommit time: %s\nmodified: %s\ngo: %s\n", v, commit, commitTime, modified, runtime.Version())
}
