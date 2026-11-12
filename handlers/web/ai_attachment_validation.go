package web

import (
	"archive/zip"
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/gofiber/fiber/v2"
)

// Inspect file signatures and Office containers without extracting document
// text. Multipart MIME headers are never used to select the model input type.
func validateAIAttachment(filename string, data []byte) (string, error) {
	if contentType, err := validateAIImage(data); err == nil {
		return contentType, nil
	}
	invalid := fiber.NewError(400, "文件格式无效，仅支持 PNG、JPEG、WebP、PDF、Word（DOC/DOCX）、Excel（XLS/XLSX） / Invalid or unsupported attachment")
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".pdf" && bytes.HasPrefix(data, []byte("%PDF-")) && bytes.Contains(data[max(0, len(data)-1024):], []byte("%%EOF")) {
		return "application/pdf", nil
	}
	if ext == ".docx" || ext == ".xlsx" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(archive.File) > 10000 {
			return "", invalid
		}
		required := "word/document.xml"
		contentType := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		if ext == ".xlsx" {
			required = "xl/workbook.xml"
			contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
		found := map[string]bool{}
		var total uint64
		for _, file := range archive.File {
			// Bound decompression work and reject corrupt/encrypted containers.
			if file.UncompressedSize64 > 100<<20 || total > (100<<20)-file.UncompressedSize64 {
				return "", invalid
			}
			total += file.UncompressedSize64
			reader, err := file.Open()
			if err != nil {
				return "", invalid
			}
			_, err = io.Copy(io.Discard, io.LimitReader(reader, int64(file.UncompressedSize64)+1))
			reader.Close()
			if err != nil {
				return "", invalid
			}
			found[file.Name] = true
		}
		if found["[Content_Types].xml"] && found[required] {
			return contentType, nil
		}
	}
	// Legacy Office files use an OLE compound container. Check directory stream
	// names as well as the signature so renaming a DOC to XLS is not accepted.
	if (ext == ".doc" || ext == ".xls") && len(data) >= 512 && bytes.Equal(data[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		hasStream := func(name string) bool {
			var encoded []byte
			for _, c := range utf16.Encode([]rune(name + "\x00")) {
				encoded = append(encoded, byte(c), byte(c>>8))
			}
			for offset := 512; offset+128 <= len(data); offset += 128 {
				entry := data[offset : offset+128]
				if entry[66] == 2 && int(entry[64]) == len(encoded) && entry[65] == 0 && bytes.HasPrefix(entry, encoded) {
					return true
				}
			}
			return false
		}
		if ext == ".doc" && hasStream("WordDocument") {
			return "application/msword", nil
		}
		if ext == ".xls" && (hasStream("Workbook") || hasStream("Book")) {
			return "application/vnd.ms-excel", nil
		}
	}
	return "", invalid
}
