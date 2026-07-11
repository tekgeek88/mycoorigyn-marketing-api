package version

var (
	Version   = "local-dev" // Default for local builds without ldflags
	GitCommit = "none"      // Useful for debugging untracked builds
	BuildTime = "unknown"   // Default for always-available runtime metadata
	GoEnv     = "unset"     // Default value

)

func Info() map[string]string {
	return map[string]string{
		"version":    Version,
		"git_commit": GitCommit,
		"build_time": BuildTime,
		"env":        GoEnv,
	}
}
