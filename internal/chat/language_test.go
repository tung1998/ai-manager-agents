package chat

import (
	"strings"
	"testing"
)

// ADR-121: the person's own language (else office's), or the one set;
// English among agents; unknown values fall back.
func TestLanguageRule(t *testing.T) {
	auto := Language{}.Rule()
	if !strings.Contains(auto, "the language they write in") || !strings.Contains(auto, "reply in Vietnamese") || !strings.Contains(auto, "use English") {
		t.Errorf("auto:\n%s", auto)
	}
	if en := (Language{System: "en"}).Rule(); !strings.Contains(en, "reply in English") {
		t.Errorf("system en:\n%s", en)
	}
	if ja := (Language{Response: "ja"}).Rule(); !strings.Contains(ja, "Always reply to the person in Japanese") {
		t.Errorf("fixed:\n%s", ja)
	}
	if l := (Language{System: "xx", Response: "klingon"}).Clean(); l.System != "vi" || l.Response != "auto" {
		t.Errorf("clean = %+v", l)
	}
}
