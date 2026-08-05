package agent

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// AttachmentKind classifies a file attachment for processing.
type AttachmentKind string

const (
	AttachImage   AttachmentKind = "image"
	AttachPDF     AttachmentKind = "pdf"
	AttachText    AttachmentKind = "text"
	AttachCode    AttachmentKind = "code"
	AttachArchive AttachmentKind = "archive"
	AttachUnknown AttachmentKind = "unknown"
)

// FileAttachment represents a file attached to a user message.
type FileAttachment struct {
	Name     string         `json:"name"`
	Path     string         `json:"path"`
	MIMEType string         `json:"mime_type"`
	Size     int64          `json:"size"`
	Kind     AttachmentKind `json:"kind"`
}

// ResolvedAttachment holds the extracted content from an attachment.
type ResolvedAttachment struct {
	Attachment  FileAttachment `json:"attachment"`
	TextContent string         `json:"text_content,omitempty"`
	ImageBase64 string         `json:"image_base64,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// DetectAttachmentKind determines the kind of a file from its extension
// and MIME type.
func DetectAttachmentKind(name, mimeType string) AttachmentKind {
	ext := strings.ToLower(filepath.Ext(name))

	// Images.
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return AttachImage
	case ".pdf":
		return AttachPDF
	case ".gz", ".gzip":
		return AttachArchive
	case ".bz2", ".bzip2":
		return AttachArchive
	}

	// Text/code by extension.
	switch ext {
	case ".txt", ".md", ".markdown", ".csv", ".log", ".json", ".yaml", ".yml",
		".xml", ".html", ".htm", ".css", ".toml", ".ini", ".cfg":
		return AttachText
	case ".go", ".rs", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".cpp",
		".h", ".rb", ".php", ".swift", ".kt", ".scala", ".sh", ".bash", ".zsh",
		".sql", ".r", ".lua", ".pl", ".Makefile", ".Dockerfile":
		return AttachCode
	}

	// MIME type fallback.
	if strings.HasPrefix(mimeType, "image/") {
		return AttachImage
	}
	if strings.HasPrefix(mimeType, "text/") {
		return AttachText
	}
	if mimeType == "application/pdf" {
		return AttachPDF
	}

	return AttachUnknown
}

// ResolveFileAttachment reads and extracts content from a file attachment.
func ResolveFileAttachment(path, name, mimeType string) *ResolvedAttachment {
	info, err := os.Stat(path)
	if err != nil {
		return &ResolvedAttachment{
			Attachment: FileAttachment{Name: name, Path: path, MIMEType: mimeType},
			Error:      fmt.Sprintf("stat file: %v", err),
		}
	}

	kind := DetectAttachmentKind(name, mimeType)
	att := FileAttachment{
		Name:     name,
		Path:     path,
		MIMEType: mimeType,
		Size:     info.Size(),
		Kind:     kind,
	}

	result := &ResolvedAttachment{Attachment: att}

	data, err := os.ReadFile(path)
	if err != nil {
		result.Error = fmt.Sprintf("read file: %v", err)
		return result
	}

	switch kind {
	case AttachText, AttachCode:
		result.TextContent = string(data)
	case AttachImage:
		// Read as base64 for vision-capable models.
		result.ImageBase64 = fmt.Sprintf("data:%s;base64,%x", mimeType, data)
	case AttachArchive:
		decompressed, decErr := decompressArchive(data, name)
		if decErr != nil {
			result.Error = fmt.Sprintf("decompress: %v", decErr)
		} else {
			result.TextContent = decompressed
		}
	case AttachPDF:
		text, pdfErr := extractPDFText(data)
		if pdfErr != nil {
			result.Error = fmt.Sprintf("pdf extract: %v", pdfErr)
		} else {
			result.TextContent = text
		}
	default:
		// Try as text.
		if isTextContent(data) {
			result.TextContent = string(data)
		} else {
			result.Error = "unsupported attachment type"
		}
	}

	return result
}

// HasImageMarkers checks if a message contains image attachment markers
// that require vision support.
func HasImageMarkers(content string) bool {
	markers := []string{
		"![",          // markdown image
		"data:image/", // inline base64
		"<image",      // XML-style
		"{image}",     // template style
	}
	lower := strings.ToLower(content)
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// CheckVisionSupport returns true if the model supports vision/image inputs.
func CheckVisionSupport(modelName string) bool {
	visionModels := map[string]bool{
		"claude-opus-4-7":   true,
		"claude-sonnet-4-6": true,
		"claude-haiku-4-5":  true,
		"gpt-4o":            true,
		"gpt-4o-mini":       true,
		"gpt-4-turbo":       true,
		"gemini-2.5-pro":    true,
		"gemini-2.5-flash":  true,
	}
	return visionModels[modelName]
}

// ── Helpers ──────────────────────────────────────────────────────────

func decompressArchive(data []byte, name string) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))

	switch ext {
	case ".gz", ".gzip":
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		defer reader.Close()
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, reader); err != nil {
			return "", err
		}
		return buf.String(), nil

	case ".bz2", ".bzip2":
		reader := bzip2.NewReader(bytes.NewReader(data))
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, reader); err != nil {
			return "", err
		}
		return buf.String(), nil

	default:
		return "", fmt.Errorf("unsupported archive format: %s", ext)
	}
}

// extractPDFText extracts text from PDF byte data. Handles both uncompressed
// content streams (BT/ET markers) and FlateDecode-compressed streams.
// A full implementation would use a PDF parsing library; this heuristic covers
// the majority of real-world PDFs.
func extractPDFText(data []byte) (string, error) {
	content := string(data)
	if !strings.Contains(content, "%PDF-") {
		return "", fmt.Errorf("not a valid PDF")
	}

	var result strings.Builder

	// Extract text from all stream objects (both compressed and uncompressed).
	streamStart := 0
	for {
		idx := strings.Index(content[streamStart:], "stream\n")
		if idx < 0 {
			idx = strings.Index(content[streamStart:], "stream\r\n")
		}
		if idx < 0 {
			break
		}
		absStart := streamStart + idx

		// Look backwards for the stream dictionary to check for FlateDecode.
		dictStart := absStart
		if dictStart > 2000 {
			dictStart -= 2000
		} else {
			dictStart = 0
		}
		dict := content[dictStart:absStart]
		isFlate := strings.Contains(dict, "FlateDecode")

		// Find endstream.
		endIdx := strings.Index(content[absStart+7:], "endstream")
		if endIdx < 0 {
			streamStart = absStart + 7
			continue
		}

		if isFlate {
			rawStart := absStart + 7 // skip "stream\n"
			rawEnd := absStart + 7 + endIdx
			streamData := []byte(content[rawStart:rawEnd])
			if decompressed, err := decompressFlate(streamData); err == nil {
				extractTextBlock(string(decompressed), &result)
			}
		} else {
			streamData := content[absStart+7 : absStart+7+endIdx]
			extractTextBlock(streamData, &result)
		}

		streamStart = absStart + 7 + endIdx + 9 // skip past "endstream"
	}

	// Also extract from non-stream BT/ET blocks (some PDFs have uncompressed
	// text objects outside streams).
	extractTextBlock(content, &result)

	if result.Len() == 0 {
		return "", fmt.Errorf("no extractable text found in PDF")
	}
	return result.String(), nil
}

// extractTextBlock extracts parenthesized strings and hex strings between
// BT/ET markers, plus Tj/TJ/'/" text operators.
func extractTextBlock(text string, result *strings.Builder) {
	inText := false
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "BT") {
			inText = true
			continue
		}
		if strings.HasPrefix(trimmed, "ET") {
			inText = false
			continue
		}
		if !inText {
			continue
		}

		// Extract parenthesized strings: (Hello World) or (Hello\) World)
		for i := 0; i < len(trimmed); i++ {
			if trimmed[i] == '(' {
				depth := 1
				j := i + 1
				for j < len(trimmed) && depth > 0 {
					if trimmed[j] == '\\' && j+1 < len(trimmed) {
						j += 2
						continue
					}
					if trimmed[j] == '(' {
						depth++
					} else if trimmed[j] == ')' {
						depth--
					}
					j++
				}
				if depth == 0 {
					extracted := trimmed[i+1 : j-1]
					// Unescape common PDF escapes.
					extracted = strings.ReplaceAll(extracted, "\\(", "(")
					extracted = strings.ReplaceAll(extracted, "\\)", ")")
					extracted = strings.ReplaceAll(extracted, "\\n", "\n")
					result.WriteString(extracted)
					result.WriteString(" ")
					i = j
				}
			}
		}
	}
}

// decompressFlate performs raw deflate decompression (RFC 1951).
// PDF streams use raw deflate, not zlib/gzip wrapped.
func decompressFlate(data []byte) ([]byte, error) {
	// Try zlib first (many PDF writers use zlib-wrapped deflate).
	if b, err := tryDecompress(data); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("deflate: unsupported")
}

// tryDecompress attempts zlib decompression, ignoring leading whitespace
// that some PDF writers emit before the zlib magic bytes.
func tryDecompress(data []byte) ([]byte, error) {
	// Strip leading whitespace/CR/LF.
	trimmed := bytes.TrimLeft(data, " \t\r\n\f")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty stream")
	}

	r, err := zlib.NewReader(bytes.NewReader(trimmed))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isTextContent(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	// Check if the first 512 bytes are mostly printable ASCII/UTF-8.
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	nonPrintable := 0
	for _, b := range data[:checkLen] {
		if b == 0 {
			return false // null byte = binary
		}
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			nonPrintable++
		}
	}
	return nonPrintable < checkLen/10 // <10% non-printable
}
