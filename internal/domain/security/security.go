// Package security sanitizes untrusted memory content and builds XML envelopes.
package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"unicode/utf8"
)

type SanitizedPayload struct {
	Summary, Content, ContentSHA256 string
	TokenCount                      int
	Sanitized                       bool
}

var ErrInvalidInput = errors.New("summary and content must be nonempty valid UTF-8 within character limits")

func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r <= 8 || r == 11 || r == 12 || (r >= 14 && r <= 31) || r == 127 || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || (r >= 0x200b && r <= 0x200f) || r == 0xfeff {
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "<romero_memory", "&lt;romero_memory")
	return strings.ReplaceAll(s, "</romero_memory>", "&lt;/romero_memory>")
}
func SanitizeInput(summary, content string) (SanitizedPayload, error) {
	if !utf8.ValidString(summary) || !utf8.ValidString(content) || utf8.RuneCountInString(summary) > 600 || utf8.RuneCountInString(content) > 65536 {
		return SanitizedPayload{}, ErrInvalidInput
	}
	s, c := clean(summary), clean(content)
	if strings.TrimSpace(s) == "" || strings.TrimSpace(c) == "" {
		return SanitizedPayload{}, ErrInvalidInput
	}
	h := sha256.Sum256([]byte(c))
	return SanitizedPayload{Summary: s, Content: c, ContentSHA256: hex.EncodeToString(h[:]), TokenCount: (utf8.RuneCountInString(c) + 3) / 4, Sanitized: s != summary || c != content}, nil
}
func escape(s string) string {
	var b strings.Builder
	// A strings.Builder cannot fail writes. EscapeText also replaces XML-invalid runes.
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func WrapUntrustedXML(id uuid.UUID, memType, epistemic string, confidence float32, sourceRef, content string) string {
	return fmt.Sprintf(`<romero_memory id="%s" type="%s" epistemic="%s" confidence="%.2f" source="%s" untrusted_content="true">%s</romero_memory>`, id, escape(memType), escape(epistemic), confidence, escape(sourceRef), escape(content))
}
