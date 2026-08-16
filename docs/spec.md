# docxedit Product Specification

Status: Implemented baseline

Audience: Maintainers and contributors

Source: Decisions reached during the initial grilling/design session

The key words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are normative.

## 1. Problem

Editing XML inside a DOCX package normally requires unpacking the archive,
finding the relevant parts, editing them, and rebuilding the package without
breaking its OPC structure. That workflow is cumbersome and can accidentally
modify or recompress unrelated package members.

`docxedit` provides the shortest safe interactive workflow for editing selected
existing `.xml` and `.rels` members in a text editor chosen by the user.

## 2. Goals

The product MUST:

- work without Microsoft Word or another Office installation;
- present existing XML parts through an interactive terminal selector;
- open selected parts together in `$EDITOR`;
- extract only selected parts to the filesystem;
- write back only parts whose bytes actually changed;
- preserve compressed payload bytes of unchanged ZIP members;
- produce a structurally valid DOCX/OPC package;
- overwrite the original path only after successful validation;
- remain fully offline and platform-oriented toward macOS, Linux, and Windows.

## 3. Non-goals

Version 1 MUST NOT:

- add, delete, rename, or move package members;
- edit package types other than `.docx`;
- support encrypted, password-protected, macro-enabled, template, or digitally
  signed Office documents;
- validate the complete WordprocessingML schema or semantic correctness of
  arbitrary user edits;
- repair an already invalid input package;
- depend on Word, LibreOffice, cloud services, or network APIs;
- provide a non-interactive scripting mode;
- provide session resume, persistent configuration, telemetry, or automatic
  updates;
- create a backup copy before replacing the original document.

## 4. Command-line contract

The command syntax is:

```text
docxedit [--editor COMMAND] FILE.docx
```

Requirements:

- Exactly one DOCX path MUST be accepted per invocation.
- Internal package paths MUST NOT be accepted as positional arguments; part
  selection is always interactive.
- The path MUST end in `.docx`, case-insensitively.
- The final path component MUST be a regular file. Symbolic links and other
  special files MUST be rejected.
- `$EDITOR` MUST supply the editor command unless `--editor` overrides it.
- If neither editor source is available, the command MUST fail with a clear
  setup message rather than guessing an editor.
- User-facing selector text, help, progress, and errors MUST be in English.
- The selector requires interactive stdin and stdout. A non-TTY invocation MUST
  fail clearly and MUST NOT prompt through another terminal device.

## 5. User workflow

The successful workflow is:

1. Resolve and preflight the input path.
2. Fingerprint the complete original DOCX.
3. Validate the input ZIP, XML, and structural OPC model.
4. Display an interactive list of existing `.xml` and `.rels` members.
5. Extract only the selected members into a private temporary workspace.
6. Invoke the editor once with all selected filesystem paths.
7. After the editor process returns, display: save the files, then press Enter.
8. On Enter, compare the selected files with their extracted byte hashes.
9. If nothing changed, clean up and leave the original completely untouched.
10. Otherwise, rebuild and validate a sibling temporary DOCX.
11. Confirm that the original fingerprint has not changed.
12. Replace the original path with the validated temporary package.
13. Clean up the workspace and report success.

There MUST be no diff or confirmation prompt on the successful path. Pressing
Enter after editing is the explicit completion signal, after which saving is
automatic if validation succeeds.

## 6. Interactive part selector

The selector MUST:

- list only existing non-directory members ending in `.xml` or `.rels`;
- sort members alphabetically by complete internal path;
- display each member's uncompressed size;
- filter case-insensitively as the user types;
- use Up and Down for navigation;
- use Space for multi-selection;
- use Enter to confirm and Escape or Ctrl-C to cancel;
- render visible `[ ]` and `[x]` states instead of relying on color alone;
- preserve selections while filtering;
- preselect `word/document.xml` when it exists.

At least one member MUST be selected before confirmation. All selected files
MUST be passed to the editor in one invocation.

## 7. Editor behavior

- `$EDITOR` and `--editor` MUST accept an executable plus quoted arguments.
- Selected file paths MUST be appended as separate arguments, not interpolated
  into a shell command.
- The editor MUST inherit the user's terminal input, output, and error streams.
- The CLI MUST wait for the editor process to return.
- The CLI MUST then wait for Enter. This supports GUI commands that hand files
  to an existing process and return before the user finishes editing.
- A non-zero editor exit MUST abort without changing the DOCX.
- Deleting or replacing a selected workspace file with a non-regular file MUST
  abort.
- Additional unselected files created by an editor, such as swap or backup
  files, MUST be ignored.

## 8. Workspace lifecycle

- The workspace MUST be created in the system temporary area with permissions
  restricted to the current user.
- The original package directory hierarchy MUST be mirrored for selected parts.
- Extracted files MUST be private to the current user.
- A successful or unchanged session MUST delete the workspace immediately.
- A failed or explicitly aborted post-edit session MUST retain the workspace
  and print its absolute path for manual recovery.
- Retained workspaces MUST NOT be automatically deleted later.
- No `--resume` mechanism is provided; cleanup of retained data is the user's
  responsibility.

## 9. Accepted DOCX and ZIP structure

Before opening the editor, the product MUST reject:

- unreadable or corrupt ZIP archives;
- encrypted ZIP members;
- unsupported compression methods;
- duplicate member paths, including case-folded duplicates;
- absolute paths, backslash paths, `..` traversal, NULs, or malformed UTF-8
  member names;
- multi-disk ZIP archives or other unsupported exotic ZIP extensions;
- digitally signed OPC packages;
- packages without a valid DOCX main part;
- packages that fail the structural validation rules below.

Stored and Deflate members MUST be supported. ZIP64 packages SHOULD be
supported when the Go ZIP implementation supports the encountered structure.

## 10. Validation contract

### 10.1 ZIP integrity

Every input member MUST be streamed fully before editing so its decompression
and CRC are verified. This does not extract unselected members to disk and does
not keep the complete archive uncompressed in memory.

The completed output archive MUST be reopened and checked in the same way.

### 10.2 XML validity

- Every `.xml` and `.rels` member MUST be well-formed XML.
- UTF-8 and UTF-16 XML MUST be accepted.
- DTD/DOCTYPE declarations MUST be rejected.
- Validation failures SHOULD include the package path, line, column, and cause
  when available.
- XML MUST NOT be reformatted, reserialized, normalized, or have its line
  endings or declared encoding changed by `docxedit`.

### 10.3 Structural OPC/DOCX validity

Validation MUST cover:

- presence and namespace of `[Content_Types].xml`;
- unique and valid Default and Override content-type declarations;
- content-type coverage for every non-directory package part;
- presence and namespace of `_rels/.rels`;
- valid relationship-part locations;
- existence of every relationship source part;
- unique, non-empty relationship IDs;
- non-empty relationship types and targets;
- resolution and existence of every internal relationship target;
- acceptance of external relationships without fetching them;
- exactly one root `officeDocument` relationship;
- a Word document main-part content type;
- Transitional and Strict OOXML office-document relationship types.

Structural validation explicitly does not guarantee that edited
WordprocessingML is schema-valid, semantically meaningful, or visually usable
in Microsoft Word.

### 10.4 Invalid output

If edited content fails XML or OPC validation:

- the original MUST remain unchanged;
- the error MUST be shown;
- the user MUST be offered a choice to reopen all selected files in the editor
  or abort;
- aborting MUST retain and report the workspace.

There MUST NOT be a `--force` option that writes known-invalid output.

## 11. Change and preservation contract

The following distinctions are normative:

| Package aspect | Required behavior |
| --- | --- |
| Unselected members | Never extracted to disk |
| Selected but unchanged members | Copied as unchanged raw compressed payloads |
| Unchanged compressed payload bytes | MUST remain byte-identical |
| Changed members | Recompressed using their original compression method |
| Member order | MUST remain unchanged |
| Member name, timestamp, comment, and attributes | MUST be preserved semantically |
| Archive comment | MUST be preserved |
| Changed member CRC and sizes | MUST be regenerated |
| ZIP local headers, central directory, descriptors, and offsets | MAY be reserialized as structurally required |
| `docProps/core.xml` or other metadata parts | MUST NOT be modified automatically |

The tool cannot promise a byte-identical archive: changing an earlier member's
size changes offsets, which necessarily changes central-directory data. The
guarantee applies to the compressed payload of each unchanged member and the
semantic preservation of exposed metadata.

After rebuilding, raw compressed hashes for every unchanged member MUST be
compared between the source and candidate archive before replacement.

## 12. Concurrent modification

- The complete input file MUST be fingerprinted before editing.
- Its regular-file status and fingerprint MUST be checked again before
  replacement.
- If the source changes during the session, replacement MUST abort and retain
  the workspace.
- The tool MUST NOT attempt an XML merge or overwrite the concurrent change.

This check reduces accidental clobbering but does not claim to eliminate every
possible filesystem time-of-check/time-of-use race.

## 13. Replacement and filesystem metadata

The new archive MUST be written to a temporary file in the same directory as
the original, closed, synchronized, and fully validated before replacement.

- On macOS and Linux local filesystems, replacement MUST use same-directory
  rename semantics so observers see either the old or new file.
- The parent directory SHOULD be synchronized after a successful Unix rename.
- On Windows, replacement SHOULD use `ReplaceFileW` to obtain the strongest
  available safe-save and target-metadata behavior.
- Windows file-sharing rules and documented partial-failure states prevent a
  universal guarantee that the original remains unchanged if the replacement
  API itself fails.
- Normal permission bits MUST be preserved.
- Ownership, ACLs, extended attributes, Finder metadata, and crash durability
  SHOULD be preserved best-effort where the operating system permits it.
- The filesystem modification time is expected to change after a successful
  content update.
- No `.bak` or other backup file MUST be created.

## 14. Privacy, network, and resource policy

- Processing MUST be local and offline.
- The tool MUST NOT make network requests, emit telemetry, or upload content.
- Temporary XML can contain confidential document data and MUST use private
  permissions.
- Version 1 intentionally has no limits on ZIP member count, compression ratio,
  individual XML size, or total uncompressed size.
- Because no resource limits exist, documentation MUST warn users to process
  only trusted documents; hostile ZIP expansion may consume excessive CPU,
  memory, or temporary disk space.

## 15. Implementation constraints

- The implementation language is Go.
- The product SHOULD build as a single executable without requiring Word or a
  language runtime on the user's machine.
- Go's `archive/zip.Writer.Copy` SHOULD be used for raw copying unchanged
  compressed members.
- The interactive selector SHOULD use a pinned Bubble Tea v2 dependency with a
  small product-specific picker model.
- OPC structural validation belongs to the product and MUST NOT delegate the
  promised validity boundary to an immature or broader OOXML library.
- The product MUST have no persistent configuration file.
- The intended personal installation mechanism is `go install
  ./cmd/docxedit`; local `go build` MUST also work.
- Version 1 does not require release binaries, package-manager recipes, or an
  automated publishing pipeline.

## 16. Testing requirements

- Runtime tests are initially required on macOS only.
- Linux and Windows portability SHOULD be preserved in code and compile checks.
- Tests MUST generate synthetic DOCX fixtures; personal documents MUST NOT be
  committed as fixtures.
- Automated tests MUST cover valid packages, malformed XML, unsafe and
  duplicate paths, missing relationship targets, signed packages, raw-copy
  preservation, command parsing, selector state, and safe replacement.
- Automated Word or LibreOffice launch tests are explicitly out of scope.
- A manual Office smoke test MAY be performed but is not part of the formal
  validity guarantee.

## 17. Acceptance criteria

The baseline is accepted when all of the following are true:

1. A valid DOCX can be opened with `docxedit FILE.docx` without Word installed.
2. The user can search for and select multiple XML parts.
3. Only selected parts appear in the private workspace.
4. All selected parts open together in the configured editor.
5. Closing without byte changes leaves the source hash and modification time
   unchanged.
6. A valid edit produces a structurally valid DOCX at the original path.
7. Every unchanged compressed member payload remains byte-identical.
8. Invalid edited XML cannot overwrite the original.
9. A concurrently modified source cannot be overwritten.
10. Failure after editing reports retained working files.
11. The complete test suite passes on macOS.

## 18. Deferred possibilities

The following MAY be considered later but are not implied by this specification:

- direct internal paths or a non-interactive mode;
- adding, deleting, or renaming package members;
- a resumable failed session;
- a repair mode for invalid input packages;
- full WordprocessingML schema validation;
- additional Office package types;
- accessibility-specific or ANSI-free selector modes;
- configurable resource limits;
- packaged releases and multi-platform CI.
