package app

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"docxedit/internal/docx"
	"docxedit/internal/picker"
	"docxedit/internal/safereplace"
)

var ErrCanceled = errors.New("canceled")

// Run executes one interactive edit session.
func Run(arguments []string, stdin *os.File, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("docxedit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	editorOverride := flags.String("editor", "", "editor command to use instead of $EDITOR")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: docxedit [--editor COMMAND] FILE.docx")
		fmt.Fprintln(flags.Output(), "Interactively select existing .xml and .rels parts, edit them, and safely update FILE.docx.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("expected exactly one DOCX file")
	}

	source, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve DOCX path: %w", err)
	}
	if !strings.EqualFold(filepath.Ext(source), ".docx") {
		return fmt.Errorf("only unencrypted .docx files are supported")
	}
	if err := requireRegularFile(source); err != nil {
		return err
	}

	editor := *editorOverride
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if strings.TrimSpace(editor) == "" {
		return fmt.Errorf("$EDITOR is not set; set it or pass --editor COMMAND")
	}
	if _, err := splitCommand(editor); err != nil {
		return fmt.Errorf("invalid editor command: %w", err)
	}

	initialFingerprint, err := fingerprint(source)
	if err != nil {
		return fmt.Errorf("fingerprint source: %w", err)
	}
	fmt.Fprintln(stdout, "Validating document...")
	parts, err := docx.Inspect(source)
	if err != nil {
		return fmt.Errorf("invalid DOCX: %w", err)
	}
	if err := ensureFingerprint(source, initialFingerprint); err != nil {
		return err
	}

	selected, err := picker.Select(parts)
	if errors.Is(err, picker.ErrCanceled) {
		fmt.Fprintln(stdout, "Canceled. The document was not changed.")
		return ErrCanceled
	}
	if err != nil {
		return err
	}

	workspace, err := os.MkdirTemp("", "docxedit-")
	if err != nil {
		return fmt.Errorf("create private workspace: %w", err)
	}
	if err := os.Chmod(workspace, 0o700); err != nil {
		os.RemoveAll(workspace)
		return fmt.Errorf("secure private workspace: %w", err)
	}
	retainWorkspace := false
	defer func() {
		if !retainWorkspace {
			_ = os.RemoveAll(workspace)
		}
	}()

	workspaceFiles, err := docx.Extract(source, workspace, selected)
	if err != nil {
		return err
	}
	originalPartFingerprints, err := fingerprintParts(workspaceFiles)
	if err != nil {
		return err
	}
	orderedWorkspaceFiles := make([]string, 0, len(selected))
	for _, name := range selected {
		orderedWorkspaceFiles = append(orderedWorkspaceFiles, workspaceFiles[name])
	}

	reader := bufio.NewReader(stdin)
	for {
		retainWorkspace = true
		if err := runEditor(editor, orderedWorkspaceFiles, stdin, stdout, stderr); err != nil {
			return withWorkspace(err, workspace)
		}
		fmt.Fprintln(stdout, "Save your changes in the editor, then press Enter to continue.")
		if _, err := reader.ReadString('\n'); err != nil {
			return withWorkspace(fmt.Errorf("wait for edit completion: %w", err), workspace)
		}

		changed, err := changedParts(workspaceFiles, originalPartFingerprints)
		if err != nil {
			return withWorkspace(err, workspace)
		}
		if len(changed) == 0 {
			retainWorkspace = false
			fmt.Fprintln(stdout, "No changes detected. The document was not rewritten.")
			return nil
		}
		if err := ensureFingerprint(source, initialFingerprint); err != nil {
			return withWorkspace(err, workspace)
		}

		temporaryArchive, err := createSiblingTemporary(source)
		if err != nil {
			return withWorkspace(err, workspace)
		}
		temporaryPath := temporaryArchive.Name()
		if err := temporaryArchive.Close(); err != nil {
			os.Remove(temporaryPath)
			return withWorkspace(fmt.Errorf("close temporary archive: %w", err), workspace)
		}

		validationErr := buildAndValidate(source, temporaryPath, changed)
		if validationErr != nil {
			_ = os.Remove(temporaryPath)
			fmt.Fprintf(stderr, "The edited document is invalid: %v\n", validationErr)
			retry, promptErr := promptRetry(reader, stdout)
			if promptErr != nil {
				return withWorkspace(promptErr, workspace)
			}
			if retry {
				continue
			}
			fmt.Fprintf(stdout, "Canceled. Working files kept at %s\n", workspace)
			return ErrCanceled
		}

		if err := ensureFingerprint(source, initialFingerprint); err != nil {
			_ = os.Remove(temporaryPath)
			return withWorkspace(err, workspace)
		}
		result, replaceErr := safereplace.Replace(temporaryPath, source)
		if replaceErr != nil {
			_ = os.Remove(temporaryPath)
			return withWorkspace(replaceErr, workspace)
		}
		if !result.Committed {
			_ = os.Remove(temporaryPath)
			return withWorkspace(fmt.Errorf("replacement did not commit"), workspace)
		}
		for _, warning := range result.Warnings {
			fmt.Fprintf(stderr, "Warning: %v\n", warning)
		}
		retainWorkspace = false
		fmt.Fprintf(stdout, "Saved %s\n", source)
		return nil
	}
}

func buildAndValidate(source, destination string, changed map[string]string) error {
	if err := docx.Rewrite(source, destination, changed); err != nil {
		return err
	}
	if err := docx.Validate(destination); err != nil {
		return err
	}
	if err := docx.VerifyRawCopies(source, destination, changed); err != nil {
		return err
	}
	return nil
}

func requireRegularFile(filename string) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("inspect DOCX: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("DOCX path must be a regular file; symbolic links are not supported")
	}
	return nil
}

func runEditor(editor string, files []string, stdin *os.File, stdout, stderr io.Writer) error {
	commandLine, err := splitCommand(editor)
	if err != nil {
		return fmt.Errorf("parse editor command: %w", err)
	}
	arguments := append(commandLine[1:], files...)
	command := exec.Command(commandLine[0], arguments...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}
	return nil
}

func splitCommand(command string) ([]string, error) {
	characters := []rune(command)
	var words []string
	var current strings.Builder
	var quote rune
	hasToken := false
	flush := func() {
		if hasToken {
			words = append(words, current.String())
			current.Reset()
			hasToken = false
		}
	}
	for index := 0; index < len(characters); index++ {
		character := characters[index]
		if character == '\\' && quote != '\'' && index+1 < len(characters) {
			next := characters[index+1]
			if next == '\\' || next == '\'' || next == '"' || (quote == 0 && unicode.IsSpace(next)) {
				current.WriteRune(next)
				hasToken = true
				index++
				continue
			}
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				current.WriteRune(character)
			}
			hasToken = true
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
			hasToken = true
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteRune(character)
			hasToken = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	if len(words) == 0 || words[0] == "" {
		return nil, fmt.Errorf("empty command")
	}
	return words, nil
}

func fingerprint(filename string) ([sha256.Size]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func ensureFingerprint(filename string, expected [sha256.Size]byte) error {
	if err := requireRegularFile(filename); err != nil {
		return err
	}
	actual, err := fingerprint(filename)
	if err != nil {
		return fmt.Errorf("fingerprint source: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("the original DOCX changed during the edit session; it was not overwritten")
	}
	return nil
}

func fingerprintParts(paths map[string]string) (map[string][sha256.Size]byte, error) {
	result := make(map[string][sha256.Size]byte, len(paths))
	for name, filename := range paths {
		value, err := fingerprint(filename)
		if err != nil {
			return nil, fmt.Errorf("fingerprint extracted part %s: %w", name, err)
		}
		result[name] = value
	}
	return result, nil
}

func changedParts(paths map[string]string, original map[string][sha256.Size]byte) (map[string]string, error) {
	changed := make(map[string]string)
	for name, filename := range paths {
		info, err := os.Lstat(filename)
		if err != nil {
			return nil, fmt.Errorf("selected part %s is missing: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("selected part %s is no longer a regular file", name)
		}
		value, err := fingerprint(filename)
		if err != nil {
			return nil, fmt.Errorf("fingerprint edited part %s: %w", name, err)
		}
		if value != original[name] {
			changed[name] = filename
		}
	}
	return changed, nil
}

func createSiblingTemporary(source string) (*os.File, error) {
	file, err := os.CreateTemp(filepath.Dir(source), ".docxedit-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary archive beside source: %w", err)
	}
	return file, nil
}

func promptRetry(reader *bufio.Reader, output io.Writer) (bool, error) {
	for {
		fmt.Fprint(output, "Edit again [e] or abort [a]? ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("read retry choice: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "e", "edit":
			return true, nil
		case "a", "abort":
			return false, nil
		}
	}
}

func withWorkspace(err error, workspace string) error {
	return fmt.Errorf("%w (working files kept at %s)", err, workspace)
}
