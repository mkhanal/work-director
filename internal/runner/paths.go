package runner

import (
	"os"
	"path/filepath"
)

// WDHome is $WD_HOME when set and non-empty, else ~/.work-director. A host
// with no home directory has nowhere to keep director state, so that fails.
func WDHome() (string, error) {
	if h := os.Getenv("WD_HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".work-director"), nil
}
