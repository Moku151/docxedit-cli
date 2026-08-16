package safereplace

import (
	"fmt"
	"os"
)

// Result separates a completed replacement from best-effort durability or
// metadata warnings that occur after the new file is already visible.
type Result struct {
	Committed bool
	Warnings  []error
}

// Replace swaps temporary over target. Both files must be in the same
// directory, and target must still be a regular, non-symlink file.
func Replace(temporary, target string) (Result, error) {
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return Result{}, fmt.Errorf("inspect target: %w", err)
	}
	if !targetInfo.Mode().IsRegular() {
		return Result{}, fmt.Errorf("target is no longer a regular file")
	}

	temporaryFile, err := os.OpenFile(temporary, os.O_RDWR, 0)
	if err != nil {
		return Result{}, fmt.Errorf("open temporary file for sync: %w", err)
	}
	warnings := copyPlatformMetadata(target, temporary)
	if err := os.Chmod(temporary, targetInfo.Mode().Perm()); err != nil {
		temporaryFile.Close()
		return Result{}, fmt.Errorf("preserve file permissions: %w", err)
	}
	syncErr := temporaryFile.Sync()
	closeErr := temporaryFile.Close()
	if syncErr != nil {
		return Result{}, fmt.Errorf("sync temporary file: %w", syncErr)
	}
	if closeErr != nil {
		return Result{}, fmt.Errorf("close temporary file: %w", closeErr)
	}

	result, err := platformReplace(temporary, target)
	result.Warnings = append(warnings, result.Warnings...)
	return result, err
}
