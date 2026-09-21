package storage

import (
	"encoding/json"
	"novelclaw/internal/model"
	"strings"
	"testing"
)

func TestReaderParagraphCompatibility(t *testing.T) {
	for _, input := range []string{
		`[" first ","","second"]`,
		`[{"text":" first "},"",{"text":"second"}]`,
		`[" first ",null,{"text":"second"}]`,
	} {
		var chapter model.ChapterContent
		if err := parseChapterJSON([]byte(`{"paragraphs":`+input+`}`), &chapter, true); err != nil {
			t.Fatal(err)
		}
		if len(chapter.SourceText) != 2 || chapter.SourceText[0] != "first" || chapter.SourceText[1] != "second" {
			t.Fatalf("unexpected paragraphs: %#v", chapter.SourceText)
		}
	}
	for _, input := range []string{`[123]`, `{"text":"invalid array"}`} {
		var chapter model.ChapterContent
		if err := parseChapterJSON([]byte(`{"paragraphs":`+input+`}`), &chapter, true); err == nil {
			t.Fatalf("accepted invalid paragraphs %s", input)
		}
	}
}

func BenchmarkReaderDecode(b *testing.B) {
	paragraphs := make([]string, 1000)
	for i := range paragraphs {
		paragraphs[i] = strings.Repeat("เนื้อหานิยายสำหรับอ่าน ", 8)
	}
	data, _ := json.Marshal(map[string]any{"title": "Chapter", "paragraphs": paragraphs})
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		var chapter model.ChapterContent
		if err := parseChapterJSON(data, &chapter, true); err != nil {
			b.Fatal(err)
		}
	}
}
