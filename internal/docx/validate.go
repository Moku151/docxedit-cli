package docx

import (
	"archive/zip"
	"fmt"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	contentTypesPart       = "[Content_Types].xml"
	contentTypesNamespace  = "http://schemas.openxmlformats.org/package/2006/content-types"
	relationshipsNamespace = "http://schemas.openxmlformats.org/package/2006/relationships"

	transitionalOfficeDocumentRelationship = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	strictOfficeDocumentRelationship       = "http://purl.oclc.org/ooxml/officeDocument/relationships/officeDocument"
	wordMainContentType                    = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
)

// Part is an editable XML member in a DOCX package.
type Part struct {
	Name             string
	UncompressedSize uint64
}

// Inspect fully validates a DOCX file and returns its editable parts.
func Inspect(filename string) ([]Part, error) {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open ZIP: %w", err)
	}
	defer reader.Close()

	if err := validateReader(&reader.Reader); err != nil {
		return nil, err
	}

	parts := make([]Part, 0)
	for _, file := range reader.File {
		if !file.FileInfo().IsDir() && isXMLPart(file.Name) {
			parts = append(parts, Part{Name: file.Name, UncompressedSize: file.UncompressedSize64})
		}
	}
	sort.Slice(parts, func(i, j int) bool {
		return strings.ToLower(parts[i].Name) < strings.ToLower(parts[j].Name)
	})
	if len(parts) == 0 {
		return nil, validationError("package", "contains no editable .xml or .rels parts")
	}
	return parts, nil
}

// Validate fully validates ZIP integrity and the structural OPC/DOCX model.
func Validate(filename string) error {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("open ZIP: %w", err)
	}
	defer reader.Close()
	return validateReader(&reader.Reader)
}

func validateReader(reader *zip.Reader) error {
	files := make(map[string]*zip.File, len(reader.File))
	filesFolded := make(map[string]string, len(reader.File))
	structuralXMLParts := make(map[string][]byte)

	for _, file := range reader.File {
		if err := validateEntry(file); err != nil {
			return err
		}
		folded := strings.ToLower(file.Name)
		if previous, exists := filesFolded[folded]; exists {
			return validationError(file.Name, fmt.Sprintf("duplicate package path (also %q)", previous))
		}
		filesFolded[folded] = file.Name
		files[file.Name] = file

		stream, err := file.Open()
		if err != nil {
			return validationError(file.Name, fmt.Sprintf("cannot open entry: %v", err))
		}
		if !file.FileInfo().IsDir() && isXMLPart(file.Name) &&
			(file.Name == contentTypesPart || strings.HasSuffix(strings.ToLower(file.Name), ".rels")) {
			data, readErr := io.ReadAll(stream)
			closeErr := stream.Close()
			if readErr != nil {
				return validationError(file.Name, fmt.Sprintf("cannot read entry: %v", readErr))
			}
			if closeErr != nil {
				return validationError(file.Name, fmt.Sprintf("cannot verify entry: %v", closeErr))
			}
			if err := validateXML(file.Name, data); err != nil {
				return err
			}
			structuralXMLParts[file.Name] = data
		} else if !file.FileInfo().IsDir() && isXMLPart(file.Name) {
			validationErr := validateXMLReader(file.Name, stream)
			closeErr := stream.Close()
			if validationErr != nil {
				return validationErr
			}
			if closeErr != nil {
				return validationError(file.Name, fmt.Sprintf("cannot close entry: %v", closeErr))
			}
		} else {
			_, copyErr := io.Copy(io.Discard, stream)
			closeErr := stream.Close()
			if copyErr != nil {
				return validationError(file.Name, fmt.Sprintf("cannot verify entry: %v", copyErr))
			}
			if closeErr != nil {
				return validationError(file.Name, fmt.Sprintf("cannot close entry: %v", closeErr))
			}
		}
	}

	contentData, ok := structuralXMLParts[contentTypesPart]
	if !ok {
		return validationError(contentTypesPart, "required part is missing")
	}
	contentTypes, err := parseContentTypes(contentData)
	if err != nil {
		return validationError(contentTypesPart, err.Error())
	}
	resolver, err := newContentTypeResolver(contentTypes)
	if err != nil {
		return validationError(contentTypesPart, err.Error())
	}
	if err := resolver.validateCoverage(files); err != nil {
		return err
	}

	if isSignedPackage(files, contentTypes) {
		return validationError("package", "digitally signed DOCX packages are not supported")
	}

	_, ok = structuralXMLParts["_rels/.rels"]
	if !ok {
		return validationError("_rels/.rels", "required package relationships part is missing")
	}

	var mainPart string
	for name, data := range structuralXMLParts {
		if !strings.HasSuffix(strings.ToLower(name), ".rels") {
			continue
		}
		relationships, err := parseRelationships(data)
		if err != nil {
			return validationError(name, err.Error())
		}
		source, err := relationshipSource(name)
		if err != nil {
			return validationError(name, err.Error())
		}
		if source != "" {
			sourceFile, exists := files[source]
			if !exists || sourceFile.FileInfo().IsDir() {
				return validationError(name, fmt.Sprintf("relationship part has missing source part %q", source))
			}
		}
		seenIDs := make(map[string]struct{}, len(relationships.Relationship))
		for _, relationship := range relationships.Relationship {
			if relationship.ID == "" || relationship.Type == "" || relationship.Target == "" {
				return validationError(name, "relationship requires non-empty Id, Type, and Target")
			}
			if _, exists := seenIDs[relationship.ID]; exists {
				return validationError(name, fmt.Sprintf("duplicate relationship Id %q", relationship.ID))
			}
			seenIDs[relationship.ID] = struct{}{}
			if strings.EqualFold(relationship.TargetMode, "External") {
				continue
			}
			target, err := resolveTarget(source, relationship.Target)
			if err != nil {
				return validationError(name, fmt.Sprintf("relationship %q: %v", relationship.ID, err))
			}
			targetFile, exists := files[target]
			if !exists || targetFile.FileInfo().IsDir() {
				return validationError(name, fmt.Sprintf("relationship %q targets missing part %q", relationship.ID, target))
			}
			if name == "_rels/.rels" && isOfficeDocumentRelationship(relationship.Type) {
				if mainPart != "" {
					return validationError(name, "multiple root officeDocument relationships")
				}
				mainPart = target
			}
		}
	}

	if mainPart == "" {
		return validationError("_rels/.rels", "root officeDocument relationship is missing")
	}
	contentType, ok := resolver.contentType(mainPart)
	if !ok || contentType != wordMainContentType {
		return validationError(mainPart, fmt.Sprintf("unexpected Word main-part content type %q", contentType))
	}
	return nil
}

func validateEntry(file *zip.File) error {
	name := file.Name
	if !utf8.ValidString(name) || name == "" || strings.ContainsRune(name, '\x00') {
		return validationError(name, "invalid ZIP entry name")
	}
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return validationError(name, "unsafe ZIP entry path")
	}
	cleanName := strings.TrimSuffix(name, "/")
	if cleanName == "" || path.Clean(cleanName) != cleanName || strings.HasPrefix(cleanName, "../") {
		return validationError(name, "unsafe ZIP entry path")
	}
	if file.Flags&0x1 != 0 {
		return validationError(name, "encrypted ZIP entries are not supported")
	}
	if file.Method != zip.Store && file.Method != zip.Deflate {
		return validationError(name, fmt.Sprintf("unsupported ZIP compression method %d", file.Method))
	}
	return nil
}

type contentTypeResolver struct {
	defaults  map[string]string
	overrides map[string]string
}

func newContentTypeResolver(document contentTypesDocument) (*contentTypeResolver, error) {
	resolver := &contentTypeResolver{defaults: make(map[string]string), overrides: make(map[string]string)}
	for _, entry := range document.Defaults {
		extension := strings.ToLower(strings.TrimPrefix(entry.Extension, "."))
		if extension == "" || entry.ContentType == "" {
			return nil, fmt.Errorf("Default requires Extension and ContentType")
		}
		if _, exists := resolver.defaults[extension]; exists {
			return nil, fmt.Errorf("duplicate Default for extension %q", extension)
		}
		resolver.defaults[extension] = entry.ContentType
	}
	for _, entry := range document.Overrides {
		if !strings.HasPrefix(entry.PartName, "/") {
			return nil, fmt.Errorf("Override PartName %q must start with /", entry.PartName)
		}
		partName := strings.TrimPrefix(entry.PartName, "/")
		if partName == "" || entry.ContentType == "" || path.Clean(partName) != partName {
			return nil, fmt.Errorf("invalid Override for part %q", entry.PartName)
		}
		if _, exists := resolver.overrides[partName]; exists {
			return nil, fmt.Errorf("duplicate Override for part %q", entry.PartName)
		}
		resolver.overrides[partName] = entry.ContentType
	}
	return resolver, nil
}

func (r *contentTypeResolver) contentType(name string) (string, bool) {
	if contentType, ok := r.overrides[name]; ok {
		return contentType, true
	}
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	contentType, ok := r.defaults[extension]
	return contentType, ok
}

func (r *contentTypeResolver) validateCoverage(files map[string]*zip.File) error {
	for name, file := range files {
		if file.FileInfo().IsDir() || name == contentTypesPart {
			continue
		}
		if _, ok := r.contentType(name); !ok {
			return validationError(contentTypesPart, fmt.Sprintf("no content type covers part %q", name))
		}
	}
	for partName := range r.overrides {
		if _, ok := files[partName]; !ok {
			return validationError(contentTypesPart, fmt.Sprintf("Override targets missing part %q", partName))
		}
	}
	return nil
}

func relationshipSource(relationshipPart string) (string, error) {
	if relationshipPart == "_rels/.rels" {
		return "", nil
	}
	directory, base := path.Split(relationshipPart)
	if !strings.HasSuffix(directory, "_rels/") || !strings.HasSuffix(base, ".rels") {
		return "", fmt.Errorf("invalid relationship-part path")
	}
	parent := strings.TrimSuffix(directory, "_rels/")
	sourceName := strings.TrimSuffix(base, ".rels")
	if sourceName == "" {
		return "", fmt.Errorf("invalid relationship-part path")
	}
	return parent + sourceName, nil
}

func resolveTarget(source, rawTarget string) (string, error) {
	parsed, err := url.Parse(rawTarget)
	if err != nil {
		return "", fmt.Errorf("invalid target URI: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" {
		return "", fmt.Errorf("internal target must be a package-relative URI")
	}
	targetPath, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", fmt.Errorf("invalid escaped target path: %w", err)
	}
	if strings.Contains(targetPath, "\\") || targetPath == "" {
		return "", fmt.Errorf("invalid target path")
	}
	if strings.HasPrefix(targetPath, "/") {
		targetPath = strings.TrimPrefix(targetPath, "/")
	} else {
		targetPath = path.Join(path.Dir(source), targetPath)
	}
	targetPath = path.Clean(targetPath)
	if targetPath == "." || strings.HasPrefix(targetPath, "../") {
		return "", fmt.Errorf("target escapes the package")
	}
	return targetPath, nil
}

func isOfficeDocumentRelationship(relationshipType string) bool {
	return relationshipType == transitionalOfficeDocumentRelationship || relationshipType == strictOfficeDocumentRelationship
}

func isSignedPackage(files map[string]*zip.File, contentTypes contentTypesDocument) bool {
	for name := range files {
		if strings.HasPrefix(strings.ToLower(name), "_xmlsignatures/") {
			return true
		}
	}
	for _, entry := range contentTypes.Defaults {
		if strings.Contains(strings.ToLower(entry.ContentType), "digital-signature") {
			return true
		}
	}
	for _, entry := range contentTypes.Overrides {
		if strings.Contains(strings.ToLower(entry.ContentType), "digital-signature") {
			return true
		}
	}
	return false
}
