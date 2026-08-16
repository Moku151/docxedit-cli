//go:build darwin || linux

package safereplace

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func copyPlatformMetadata(source, destination string) []error {
	warnings := make([]error, 0)
	var stat unix.Stat_t
	if err := unix.Stat(source, &stat); err != nil {
		warnings = append(warnings, fmt.Errorf("read ownership: %w", err))
	} else if err := os.Chown(destination, int(stat.Uid), int(stat.Gid)); err != nil {
		warnings = append(warnings, fmt.Errorf("preserve ownership: %w", err))
	}

	size, err := unix.Listxattr(source, nil)
	if err != nil {
		warnings = append(warnings, fmt.Errorf("list extended attributes: %w", err))
		return warnings
	}
	if size == 0 {
		return warnings
	}
	buffer := make([]byte, size)
	size, err = unix.Listxattr(source, buffer)
	if err != nil {
		warnings = append(warnings, fmt.Errorf("list extended attributes: %w", err))
		return warnings
	}
	for _, name := range strings.Split(string(buffer[:size]), "\x00") {
		if name == "" {
			continue
		}
		valueSize, err := unix.Getxattr(source, name, nil)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("read extended attribute %q: %w", name, err))
			continue
		}
		value := make([]byte, valueSize)
		valueSize, err = unix.Getxattr(source, name, value)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("read extended attribute %q: %w", name, err))
			continue
		}
		if err := unix.Setxattr(destination, name, value[:valueSize], 0); err != nil {
			warnings = append(warnings, fmt.Errorf("preserve extended attribute %q: %w", name, err))
		}
	}
	return warnings
}
