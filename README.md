# docxedit

`docxedit` opens selected XML parts from a `.docx` package in `$EDITOR` and
writes only changed parts back. It does not unpack the complete document to
disk and does not require Microsoft Word.

The tool is designed to be portable Go code. CI tests it on macOS, Linux, and
Windows and cross-compiles downloads for `amd64` and `arm64`.

## Install

### Prebuilt continuous build

The `continuous` release is a successfully verified development build from
`main`; obsolete workflow runs never replace a newer result. It is not a stable
version and makes no compatibility promise.

| Platform | Architecture | Download |
| --- | --- | --- |
| macOS | Intel (`amd64`) | [tar.gz](https://github.com/Moku151/docxedit-cli/releases/download/continuous/docxedit_continuous_darwin_amd64.tar.gz) |
| macOS | Apple Silicon (`arm64`) | [tar.gz](https://github.com/Moku151/docxedit-cli/releases/download/continuous/docxedit_continuous_darwin_arm64.tar.gz) |
| Linux | `amd64` | [tar.gz](https://github.com/Moku151/docxedit-cli/releases/download/continuous/docxedit_continuous_linux_amd64.tar.gz) |
| Linux | `arm64` | [tar.gz](https://github.com/Moku151/docxedit-cli/releases/download/continuous/docxedit_continuous_linux_arm64.tar.gz) |
| Windows | `amd64` | [zip](https://github.com/Moku151/docxedit-cli/releases/download/continuous/docxedit_continuous_windows_amd64.zip) |
| Windows | `arm64` | [zip](https://github.com/Moku151/docxedit-cli/releases/download/continuous/docxedit_continuous_windows_arm64.zip) |

Each release also provides [SHA256SUMS](https://github.com/Moku151/docxedit-cli/releases/download/continuous/SHA256SUMS).
Compare the checksum before extracting the archive:

```sh
# Linux
sha256sum --check --ignore-missing SHA256SUMS

# macOS (compare the result with the matching SHA256SUMS line)
shasum -a 256 docxedit_continuous_darwin_arm64.tar.gz
```

On Windows, use `Get-FileHash ARCHIVE -Algorithm SHA256` in PowerShell. GitHub
build provenance can be verified on every platform with the GitHub CLI:

```sh
gh attestation verify ARCHIVE -R Moku151/docxedit-cli
```

After extraction, `docxedit --version` prints the exact source commit embedded
in the downloaded executable.

### Build from source

Go 1.26 or newer is required to build the current source tree.

```sh
go install ./cmd/docxedit
```

By default, `go install` places the executable in `$(go env GOPATH)/bin`. Add
that directory to `PATH` so `docxedit` can be run from any working directory:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
command -v docxedit
docxedit --help
```

To keep this setting on macOS with zsh, add the `export PATH=...` line to
`~/.zshrc` and open a new terminal (or run `source ~/.zshrc`). If `GOBIN` is
customized, add the directory printed by `go env GOBIN` instead.

Alternatively, build a local executable:

```sh
go build -o docxedit ./cmd/docxedit
```

Set the editor command through `EDITOR`. Arguments and quoted executable paths
are supported:

```sh
export EDITOR='vim'
export EDITOR='code --wait'
export EDITOR='open -a "Visual Studio Code"'
```

The command can also be overridden for one invocation:

```sh
docxedit --editor 'code --wait' document.docx
```

## Use

```sh
docxedit document.docx
```

The selector shows existing `.xml` and `.rels` package parts. Its controls are:

- type to filter paths;
- Up/Down to move;
- Space to toggle a part;
- Enter to open all selected parts together;
- Escape to cancel.

`word/document.xml` is preselected when present. Selected files are written to
a private temporary directory with their package directory structure intact.
After the editor command returns, `docxedit` waits for Enter. This also works
with graphical editor commands that return before their window closes: save in
the editor first, then press Enter in the terminal.

If no bytes changed, the original DOCX—including its modification time—is left
untouched.

## Safety model

Before editing, `docxedit`:

- requires one regular, non-symlink `.docx` file;
- rejects encrypted members, unsafe or duplicate paths, unsupported compression
  methods, digital signatures, and non-DOCX OPC packages;
- streams every ZIP member to verify its CRC;
- checks that all XML is well-formed;
- validates content-type coverage, relationship IDs and targets, and the root
  Word main-part relationship for Transitional or Strict OOXML.

After editing, the replacement archive is built beside the original. Changed
parts retain their original ZIP order, compression method, timestamp, and
attributes. Unchanged members are copied as their original compressed payload
bytes without decompression or recompression. ZIP headers, the central
directory, and offsets are necessarily serialized again.

The complete result is then validated again, and raw hashes confirm that every
unchanged compressed payload stayed byte-identical. The original document is
fingerprinted before and immediately before replacement; a concurrent change
causes an abort.

On macOS and Linux, a same-directory rename atomically makes the validated
archive visible. Windows uses `ReplaceFileW`; Windows file-sharing rules and
the operating system's documented partial-failure cases still apply. No backup
is created. POSIX permission bits are preserved where supported, while
ownership, ACLs, extended attributes, and crash durability are preserved
best-effort where supported.

If validation fails, the editor can be reopened. On abort or another error,
the absolute path of the retained working files is printed. There is no resume
command; remove that directory manually when it is no longer needed. Successful
and unchanged sessions clean it up automatically.

## Deliberate limits

- Only existing `.xml` and `.rels` members can be edited. Members cannot be
  added, deleted, or renamed.
- Only unencrypted `.docx` packages are accepted. `.docm`, `.dotx`, signed
  packages, and unusual ZIP extensions are rejected.
- Validation covers ZIP integrity and structural OPC/DOCX consistency. It does
  not validate every WordprocessingML schema or guarantee that arbitrary XML
  changes make semantic sense to Microsoft Word.
- The tool is offline and has no telemetry.
- The current design intentionally has no entry-count or uncompressed-size
  limits. Only use documents from trusted sources; malicious ZIP expansion can
  consume excessive CPU, memory, or temporary disk space.
- Selection requires an interactive terminal. There is no scripting mode or
  persistent configuration file.

## Development

```sh
go test ./...
go vet ./...
```

Tests generate their DOCX fixtures from scratch; no personal documents are
stored in the repository.
