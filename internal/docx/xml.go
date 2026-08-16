package docx

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

func validateXML(part string, data []byte) error {
	return validateXMLReader(part, bytes.NewReader(data))
}

func validateXMLReader(part string, reader io.Reader) error {
	decoder, err := newXMLDecoder(reader)
	if err != nil {
		return &ValidationError{Part: part, Message: fmt.Sprintf("prepare XML decoder: %v", err)}
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			line, column := decoder.InputPos()
			return &ValidationError{
				Part:    part,
				Line:    line,
				Column:  column,
				Message: fmt.Sprintf("invalid XML: %v", err),
			}
		}
		if directive, ok := token.(xml.Directive); ok && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(string(directive))), "DOCTYPE") {
			line, column := decoder.InputPos()
			return &ValidationError{Part: part, Line: line, Column: column, Message: "DOCTYPE declarations are not allowed in DOCX XML"}
		}
	}
}

func newXMLDecoder(reader io.Reader) (*xml.Decoder, error) {
	buffered := bufio.NewReader(reader)
	prefix, _ := buffered.Peek(2)
	var decoder *xml.Decoder
	if len(prefix) == 2 && ((prefix[0] == 0xff && prefix[1] == 0xfe) || (prefix[0] == 0xfe && prefix[1] == 0xff)) {
		data, err := io.ReadAll(buffered)
		if err != nil {
			return nil, fmt.Errorf("read XML: %w", err)
		}
		decoded, err := decodeUTF16(data)
		if err != nil {
			return nil, fmt.Errorf("invalid UTF-16 XML: %w", err)
		}
		decoder = xml.NewDecoder(strings.NewReader(decoded))
		decoder.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
			normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(label), "_", "-"))
			if strings.HasPrefix(normalized, "utf-16") {
				return input, nil
			}
			return xmlCharsetReader(label, input)
		}
	} else {
		decoder = xml.NewDecoder(buffered)
		decoder.CharsetReader = xmlCharsetReader
	}
	return decoder, nil
}

func decodeUTF16(data []byte) (string, error) {
	if len(data) < 2 || len(data)%2 != 0 {
		return "", fmt.Errorf("invalid byte length")
	}
	var order binary.ByteOrder
	switch {
	case data[0] == 0xfe && data[1] == 0xff:
		order = binary.BigEndian
	case data[0] == 0xff && data[1] == 0xfe:
		order = binary.LittleEndian
	default:
		return "", fmt.Errorf("byte-order mark is missing")
	}
	data = data[2:]
	codeUnits := make([]uint16, len(data)/2)
	for index := range codeUnits {
		codeUnits[index] = order.Uint16(data[index*2 : index*2+2])
	}
	return string(utf16.Decode(codeUnits)), nil
}

func xmlCharsetReader(label string, input io.Reader) (io.Reader, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(label), "_", "-"))
	if normalized == "us-ascii" || normalized == "ascii" {
		return input, nil
	}
	if normalized != "utf-16" && normalized != "utf-16le" && normalized != "utf-16be" {
		return nil, fmt.Errorf("unsupported XML encoding %q (DOCX XML must use UTF-8 or UTF-16)", label)
	}
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("odd-length UTF-16 input")
	}
	var order binary.ByteOrder = binary.LittleEndian
	if normalized == "utf-16be" {
		order = binary.BigEndian
	}
	if len(data) >= 2 {
		switch {
		case data[0] == 0xfe && data[1] == 0xff:
			order = binary.BigEndian
			data = data[2:]
		case data[0] == 0xff && data[1] == 0xfe:
			order = binary.LittleEndian
			data = data[2:]
		}
	}
	codeUnits := make([]uint16, len(data)/2)
	for index := range codeUnits {
		codeUnits[index] = order.Uint16(data[index*2 : index*2+2])
	}
	return strings.NewReader(string(utf16.Decode(codeUnits))), nil
}

type contentTypesDocument struct {
	XMLName   xml.Name              `xml:"Types"`
	Defaults  []contentTypeDefault  `xml:"Default"`
	Overrides []contentTypeOverride `xml:"Override"`
}

type contentTypeDefault struct {
	Extension   string `xml:"Extension,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type contentTypeOverride struct {
	PartName    string `xml:"PartName,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type relationshipsDocument struct {
	XMLName      xml.Name       `xml:"Relationships"`
	Relationship []relationship `xml:"Relationship"`
}

type relationship struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

func parseContentTypes(data []byte) (contentTypesDocument, error) {
	var document contentTypesDocument
	decoder, err := newXMLDecoder(bytes.NewReader(data))
	if err != nil {
		return document, err
	}
	if err := decoder.Decode(&document); err != nil {
		return document, err
	}
	if document.XMLName.Space != contentTypesNamespace {
		return document, fmt.Errorf("unexpected content-types namespace %q", document.XMLName.Space)
	}
	return document, nil
}

func parseRelationships(data []byte) (relationshipsDocument, error) {
	var document relationshipsDocument
	decoder, err := newXMLDecoder(bytes.NewReader(data))
	if err != nil {
		return document, err
	}
	if err := decoder.Decode(&document); err != nil {
		return document, err
	}
	if document.XMLName.Space != relationshipsNamespace {
		return document, fmt.Errorf("unexpected relationships namespace %q", document.XMLName.Space)
	}
	return document, nil
}

func isXMLPart(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels")
}
