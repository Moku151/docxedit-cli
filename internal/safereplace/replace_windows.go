//go:build windows

package safereplace

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func platformReplace(temporary, target string) (Result, error) {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return Result{}, fmt.Errorf("encode target path: %w", err)
	}
	temporaryPointer, err := windows.UTF16PtrFromString(temporary)
	if err != nil {
		return Result{}, fmt.Errorf("encode temporary path: %w", err)
	}
	result, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		uintptr(unsafe.Pointer(temporaryPointer)),
		0,
		0,
		0,
		0,
	)
	if result == 0 {
		if callErr == nil || callErr == syscall.Errno(0) {
			callErr = fmt.Errorf("unknown Windows error")
		}
		return Result{}, fmt.Errorf("ReplaceFileW failed (close applications using the document and retry): %w", callErr)
	}

	replaceResult := Result{Committed: true}
	file, err := os.OpenFile(target, os.O_RDWR, 0)
	if err != nil {
		replaceResult.Warnings = append(replaceResult.Warnings, fmt.Errorf("open replaced file for sync: %w", err))
		return replaceResult, nil
	}
	if err := file.Sync(); err != nil {
		replaceResult.Warnings = append(replaceResult.Warnings, fmt.Errorf("sync replaced file: %w", err))
	}
	if err := file.Close(); err != nil {
		replaceResult.Warnings = append(replaceResult.Warnings, fmt.Errorf("close replaced file: %w", err))
	}
	return replaceResult, nil
}
