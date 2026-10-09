package experience

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func UploadContentType(purpose, name string, data []byte, detectedType string) (string, error) {
	kind := strings.Split(detectedType, ";")[0]
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".svg" {
		if err := validateSVG(data); err != nil {
			return "", err
		}
		return "image/svg+xml", nil
	}
	images := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon"}
	if expected, ok := images[ext]; ok && kind == expected {
		return kind, nil
	}
	if purpose == "ticket" {
		if ext == ".pdf" && kind == "application/pdf" {
			return kind, nil
		}
		if ext == ".zip" && kind == "application/zip" {
			return kind, nil
		}
		if (ext == ".txt" || ext == ".log") && kind == "text/plain" && utf8.Valid(data) {
			return "text/plain; charset=utf-8", nil
		}
	}
	return "", fmt.Errorf("%w: unsupported file format or content does not match its extension", ErrInvalid)
}

// SVG is served with a restrictive CSP too. Reject active elements, document
// declarations, event handlers and external references instead of rewriting it.
func validateSVG(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	root, depth, count := false, 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: invalid SVG", ErrInvalid)
		}
		switch value := token.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: SVG document declarations are not allowed", ErrInvalid)
		case xml.ProcInst:
			if value.Target != "xml" || root {
				return fmt.Errorf("%w: SVG processing instructions are not allowed", ErrInvalid)
			}
		case xml.StartElement:
			name := strings.ToLower(value.Name.Local)
			if !root {
				if name != "svg" || (value.Name.Space != "" && value.Name.Space != "http://www.w3.org/2000/svg") {
					return fmt.Errorf("%w: SVG root is required", ErrInvalid)
				}
				root = true
			} else if depth == 0 {
				return fmt.Errorf("%w: multiple SVG roots", ErrInvalid)
			}
			depth++
			count++
			if depth > 64 || count > 10000 || name == "script" || name == "foreignobject" || name == "style" || strings.HasPrefix(name, "animate") || name == "set" {
				return fmt.Errorf("%w: active or excessively complex SVG is not allowed", ErrInvalid)
			}
			for _, attr := range value.Attr {
				key, content := strings.ToLower(attr.Name.Local), strings.TrimSpace(strings.ToLower(attr.Value))
				if strings.HasPrefix(key, "on") || key == "base" || strings.Contains(content, "javascript:") || strings.Contains(content, "data:") || strings.Contains(content, "@import") || strings.Contains(content, "url(") || ((key == "href" || key == "src") && !strings.HasPrefix(content, "#")) {
					return fmt.Errorf("%w: unsafe SVG attribute", ErrInvalid)
				}
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return fmt.Errorf("%w: invalid SVG content", ErrInvalid)
			}
		}
	}
	if !root || depth != 0 {
		return fmt.Errorf("%w: invalid SVG", ErrInvalid)
	}
	return nil
}
