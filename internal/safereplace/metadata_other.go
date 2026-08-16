//go:build !darwin && !linux

package safereplace

func copyPlatformMetadata(_, _ string) []error { return nil }
