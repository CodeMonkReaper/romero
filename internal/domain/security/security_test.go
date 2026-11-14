package security_test

import (
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/security"
	"strings"
	"testing"
)

func TestSanitizeAndWrap(t *testing.T) {
	p, err := security.SanitizeInput("summary", "a\x00\u202eb<romero_memory>c</romero_memory>")
	if err != nil {
		t.Fatal(err)
	}
	if p.Content != "ab&lt;romero_memory>c&lt;/romero_memory>" || !p.Sanitized || len(p.ContentSHA256) != 64 {
		t.Errorf("unexpected payload: %+v", p)
	}
	x := security.WrapUntrustedXML(uuid.New(), "semantic", "observed", 0.85, "source", "</romero_memory><evil/>")
	if !strings.Contains(x, `untrusted_content="true"`) || strings.Contains(x, "<evil/>") {
		t.Errorf("unsafe envelope: %s", x)
	}
}

func TestSanitizationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, summary, content string
		invalid                bool
	}{
		{"empty summary", "", "body", true}, {"empty content", "summary", "", true}, {"only controls", "\x00\u200b", "body", true},
		{"long summary", strings.Repeat("é", 601), "body", true}, {"long body", "summary", strings.Repeat("界", 65537), true},
		{"max unicode", strings.Repeat("é", 600), strings.Repeat("界", 65536), false}, {"bad UTF8", "summary", string([]byte{0xff}), true},
		{"whitespace", "summary", " \t\r\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := security.SanitizeInput(tc.summary, tc.content)
			if errors.Is(err, security.ErrInvalidInput) != tc.invalid {
				t.Fatalf("error=%v invalid=%v", err, tc.invalid)
			}
			if !tc.invalid && (p.Summary != tc.summary || p.Content != tc.content || p.Sanitized) {
				t.Errorf("clean content changed: %+v", p)
			}
		})
	}
	var controls strings.Builder
	for r := rune(0); r <= 127; r++ {
		if r <= 8 || r == 11 || r == 12 || (r >= 14 && r <= 31) || r == 127 {
			controls.WriteRune(r)
		}
	}
	for _, pair := range [][2]rune{{0x202a, 0x202e}, {0x2066, 0x2069}, {0x200b, 0x200f}, {0xfeff, 0xfeff}} {
		for r := pair[0]; r <= pair[1]; r++ {
			controls.WriteRune(r)
		}
	}
	p, err := security.SanitizeInput("summary", "a"+controls.String()+"\t\n\rb")
	if err != nil {
		t.Fatal(err)
	}
	if p.Content != "a\t\n\rb" || !p.Sanitized {
		t.Errorf("controls not sanitized: %+v", p)
	}
	h := sha256.Sum256([]byte(p.Content))
	if p.ContentSHA256 != fmt.Sprintf("%x", h) {
		t.Error("hash must follow sanitization")
	}
	p2, err := security.SanitizeInput(p.Summary, p.Content)
	if err != nil || p2.Content != p.Content || p2.ContentSHA256 != p.ContentSHA256 || p2.Sanitized {
		t.Error("sanitization must be idempotent")
	}
}

func TestSanitizedLengthLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   int
		summary bool
	}{
		{"summary", 600, true},
		{"content", 65536, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, marker := range []string{"<romero_memory>", "</romero_memory>"} {
				for _, extra := range []int{0, 1, 3} {
					input := strings.Repeat("é", tc.limit-3+extra-len(marker)) + marker
					summary, content := "summary", "body"
					if tc.summary {
						summary = input
					} else {
						content = input
					}
					p, err := security.SanitizeInput(summary, content)
					if extra > 0 {
						if !errors.Is(err, security.ErrInvalidInput) {
							t.Fatalf("marker=%q extra=%d: expected length rejection, got %v", marker, extra, err)
						}
					} else if err != nil || !p.Sanitized {
						t.Fatalf("exact post-clean limit must succeed: payload=%+v err=%v", p, err)
					}
				}
			}
		})
	}
}

func TestXMLRoundTrip(t *testing.T) {
	for _, body := range []string{"</romero_memory><evil/>", "<![CDATA[x]]>", "a&b", "<romero_memory", "\x00\xff\u202e"} {
		id := uuid.New()
		x := security.WrapUntrustedXML(id, `a"<&`, "observed", 0.85, `source"<&`, body)
		var doc struct {
			XMLName    xml.Name
			ID         string `xml:"id,attr"`
			Type       string `xml:"type,attr"`
			Source     string `xml:"source,attr"`
			Untrusted  string `xml:"untrusted_content,attr"`
			Confidence string `xml:"confidence,attr"`
			Body       string `xml:",chardata"`
		}
		if err := xml.Unmarshal([]byte(x), &doc); err != nil {
			t.Fatalf("invalid XML %q: %v", x, err)
		}
		if doc.XMLName.Local != "romero_memory" || doc.ID != id.String() || doc.Type != `a"<&` || doc.Source != `source"<&` || doc.Untrusted != "true" || doc.Confidence != "0.85" {
			t.Errorf("incorrect envelope: %+v", doc)
		}
		if strings.Contains(body, "\x00") {
			continue
		}
		if doc.Body != body {
			t.Errorf("body changed: %q -> %q", body, doc.Body)
		}
	}
}
