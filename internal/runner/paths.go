package runner

import (
	"os"
	"path/filepath"
)

// home is $HOME, matching the TS `process.env.HOME ?? ”`.
func home() string {
	h, _ := os.LookupEnv("HOME")
	return h
}

// wdHome is $WD_HOME when set, else ~/.work-director.
func wdHome() string {
	if h, ok := os.LookupEnv("WD_HOME"); ok {
		return h
	}
	return filepath.Join(home(), ".work-director")
}
