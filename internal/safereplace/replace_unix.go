//go:build darwin || linux

package safereplace

import (
	"fmt"
	"os"
	"path/filepath"
)

func platformReplace(temporary, target string) (Result, error) {
	if err := os.Rename(temporary, target); err != nil {
		return Result{}, fmt.Errorf("atomically replace target: %w", err)
	}
	result := Result{Committed: true}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Errorf("open parent directory for sync: %w", err))
		return result, nil
	}
	if err := directory.Sync(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Errorf("sync parent directory: %w", err))
	}
	if err := directory.Close(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Errorf("close parent directory: %w", err))
	}
	return result, nil
}
