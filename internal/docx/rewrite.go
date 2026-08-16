package docx

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Extract copies only the selected parts to workspace and returns their paths.
func Extract(source, workspace string, selected []string) (map[string]string, error) {
	wanted := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		wanted[name] = struct{}{}
	}

	reader, err := zip.OpenReader(source)
	if err != nil {
		return nil, fmt.Errorf("open ZIP: %w", err)
	}
	defer reader.Close()

	paths := make(map[string]string, len(selected))
	for _, file := range reader.File {
		if _, ok := wanted[file.Name]; !ok {
			continue
		}
		destination := filepath.Join(workspace, filepath.FromSlash(file.Name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return nil, fmt.Errorf("create workspace directory: %w", err)
		}
		input, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", file.Name, err)
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			input.Close()
			return nil, fmt.Errorf("create workspace file %s: %w", file.Name, err)
		}
		_, copyErr := io.Copy(output, input)
		closeOutputErr := output.Close()
		closeInputErr := input.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("extract %s: %w", file.Name, copyErr)
		}
		if closeOutputErr != nil {
			return nil, fmt.Errorf("close workspace file %s: %w", file.Name, closeOutputErr)
		}
		if closeInputErr != nil {
			return nil, fmt.Errorf("close source part %s: %w", file.Name, closeInputErr)
		}
		paths[file.Name] = destination
	}
	if len(paths) != len(wanted) {
		return nil, fmt.Errorf("one or more selected parts disappeared from the package")
	}
	return paths, nil
}

// Rewrite builds destination from source. Unchanged members are copied as raw
// compressed streams; only entries present in changed are recompressed.
func Rewrite(source, destination string, changed map[string]string) (err error) {
	input, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("open source ZIP: %w", err)
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fmt.Errorf("open destination ZIP: %w", err)
	}
	defer func() {
		if closeErr := output.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	writer := zip.NewWriter(output)
	if input.Comment != "" {
		if err := writer.SetComment(input.Comment); err != nil {
			return fmt.Errorf("copy ZIP comment: %w", err)
		}
	}
	for _, file := range input.File {
		workspacePath, isChanged := changed[file.Name]
		if !isChanged {
			if err := writer.Copy(file); err != nil {
				return fmt.Errorf("copy unchanged part %s: %w", file.Name, err)
			}
			continue
		}
		header := file.FileHeader
		header.CRC32 = 0
		header.CompressedSize = 0
		header.CompressedSize64 = 0
		header.UncompressedSize = 0
		header.UncompressedSize64 = 0
		header.Extra = withoutExtraField(header.Extra, 0x0001)
		destinationPart, err := writer.CreateHeader(&header)
		if err != nil {
			return fmt.Errorf("create changed part %s: %w", file.Name, err)
		}
		sourcePart, err := os.Open(workspacePath)
		if err != nil {
			return fmt.Errorf("open changed part %s: %w", file.Name, err)
		}
		_, copyErr := io.Copy(destinationPart, sourcePart)
		closeErr := sourcePart.Close()
		if copyErr != nil {
			return fmt.Errorf("write changed part %s: %w", file.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close changed part %s: %w", file.Name, closeErr)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish ZIP: %w", err)
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("sync ZIP: %w", err)
	}
	return nil
}

func withoutExtraField(extra []byte, fieldID uint16) []byte {
	result := make([]byte, 0, len(extra))
	for len(extra) >= 4 {
		id := binary.LittleEndian.Uint16(extra[0:2])
		size := int(binary.LittleEndian.Uint16(extra[2:4]))
		if size > len(extra)-4 {
			return append(result, extra...)
		}
		field := extra[:4+size]
		if id != fieldID {
			result = append(result, field...)
		}
		extra = extra[4+size:]
	}
	return append(result, extra...)
}

// VerifyRawCopies checks that compressed payload bytes for unchanged entries
// are identical before and after rewriting.
func VerifyRawCopies(source, destination string, changed map[string]string) error {
	before, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("open source ZIP: %w", err)
	}
	defer before.Close()
	after, err := zip.OpenReader(destination)
	if err != nil {
		return fmt.Errorf("open rewritten ZIP: %w", err)
	}
	defer after.Close()

	if len(before.File) != len(after.File) {
		return fmt.Errorf("rewritten ZIP changed the entry count")
	}
	for index, oldFile := range before.File {
		newFile := after.File[index]
		if oldFile.Name != newFile.Name {
			return fmt.Errorf("rewritten ZIP changed entry order at %q", oldFile.Name)
		}
		if _, isChanged := changed[oldFile.Name]; isChanged {
			continue
		}
		oldHash, err := rawHash(oldFile)
		if err != nil {
			return fmt.Errorf("hash source part %s: %w", oldFile.Name, err)
		}
		newHash, err := rawHash(newFile)
		if err != nil {
			return fmt.Errorf("hash rewritten part %s: %w", newFile.Name, err)
		}
		if oldHash != newHash {
			return fmt.Errorf("compressed payload changed for unchanged part %s", oldFile.Name)
		}
	}
	return nil
}

func rawHash(file *zip.File) ([sha256.Size]byte, error) {
	stream, err := file.OpenRaw()
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, stream); err != nil {
		return [sha256.Size]byte{}, err
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}
