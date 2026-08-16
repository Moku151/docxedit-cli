//go:build !darwin && !linux && !windows

package safereplace

import (
	"fmt"
	"os"
)

func platformReplace(temporary, target string) (Result, error) {
	if err := os.Rename(temporary, target); err != nil {
		return Result{}, fmt.Errorf("replace target: %w", err)
	}
	return Result{Committed: true}, nil
}
