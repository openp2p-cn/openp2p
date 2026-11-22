package openp2p

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// workdirFromArgs extracts -workdir/--workdir from an argument list. It is
// parsed before the regular flag set because the process must chdir into the
// work directory before opening config.json and the log file.
func workdirFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-workdir" || arg == "--workdir":
			if i+1 < len(args) {
				return strings.TrimSpace(args[i+1])
			}
		case strings.HasPrefix(arg, "-workdir="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "-workdir="))
		case strings.HasPrefix(arg, "--workdir="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "--workdir="))
		}
	}
	return ""
}

// resolveWorkdir picks the work directory: flag > environment > fallback.
func resolveWorkdir(fallback string, args []string) string {
	if dir := workdirFromArgs(args); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("OPENP2P_WORKDIR")); dir != "" {
		return dir
	}
	return fallback
}

// prepareWorkdir creates the directory (0700) and changes into it. All
// relative paths used by the daemon (config.json, log/) resolve there.
func prepareWorkdir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty work directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve work directory %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return fmt.Errorf("create work directory %q: %w", abs, err)
	}
	if err := os.Chdir(abs); err != nil {
		return fmt.Errorf("change into work directory %q: %w", abs, err)
	}
	return nil
}
