package translator

import (
	"fmt"
	"strings"
	"testing"

	"novelclaw/internal/model"
)

func TestBuildSystemPrompt_All9Genres(t *testing.T) {
	genres := []struct {
		genre    string
		expected string
	}{
		{"apocalypse", "วันสิ้นโลก"},
		{"survival", "วันสิ้นโลก"},
		{"xianxia", "ยุทธภพ"},
		{"wuxia", "ยุทธภพ"},
		{"cultivation", "ยุทธภพ"},
		{"system", "ดันเจี้ยน"},
		{"game", "ดันเจี้ยน"},
		{"lord", "ดันเจี้ยน"},
		{"fantasy", "แฟนตาซีตะวันตก"},
		{"magic", "แฟนตาซีตะวันตก"},
		{"urban", "ชีวิตในเมือง"},
		{"modern", "ชีวิตในเมือง"},
		{"doctor", "ชีวิตในเมือง"},
		{"scifi", "ไซไฟ"},
		{"space", "ไซไฟ"},
		{"mecha", "ไซไฟ"},
		{"historical", "ย้อนยุค"},
		{"rebirth", "ย้อนยุค"},
		{"palace", "ย้อนยุค"},
		{"horror", "ระทึกขวัญ"},
		{"mystery", "ระทึกขวัญ"},
		{"supernatural", "ระทึกขวัญ"},
		{"romance", "โรแมนติก"},
		{"drama", "โรแมนติก"},
	}

	for _, tc := range genres {
		prompt := BuildSystemPrompt(nil, "", tc.genre, "")
		if !strings.Contains(prompt, tc.expected) {
			t.Errorf("Genre '%s' expected to contain '%s', but got:\n%s", tc.genre, tc.expected, prompt)
		}
	}
}

func TestBuildSystemPrompt_WithGlossary(t *testing.T) {
	g := &model.NovelGlossary{
		Terms: []model.GlossaryItem{
			{Term: "曹星", Target: "เฉาซิง", Category: "character", Notes: "ตัวเอก"},
			{Term: "冰封纪元", Target: "ยุคน้ำแข็ง", Category: "custom"},
		},
	}
	prompt := BuildSystemPrompt(g, "", "system", "")

	if !strings.Contains(prompt, "曹星 -> เฉาซิง") {
		t.Error("Glossary term with notes not found in prompt")
	}
	if !strings.Contains(prompt, "冰封纪元 -> ยุคน้ำแข็ง") {
		t.Error("Glossary term without notes not found in prompt")
	}
	if !strings.Contains(prompt, "ตัวเอก") {
		t.Error("Glossary notes not found in prompt")
	}
}

func TestBuildSystemPrompt_WithContext(t *testing.T) {
	prompt := BuildSystemPrompt(nil, "ตอนก่อนหน้า: เฉาซิงพบสมบัติ", "apocalypse", "")

	if !strings.Contains(prompt, "เฉาซิงพบสมบัติ") {
		t.Error("Previous context not injected into prompt")
	}
	if !strings.Contains(prompt, "บริบทสรุปจากตอนก่อนหน้า") {
		t.Error("Context header not found in prompt")
	}
}

func TestBuildUserPrompt(t *testing.T) {
	paras := []string{"第一段", "第二段", "第三段"}
	result := BuildUserPrompt("第一章 开始", paras)

	if !strings.Contains(result, "第一章 开始") {
		t.Error("Title not included in user prompt")
	}
	if !strings.Contains(result, "3 ย่อหน้า") {
		t.Error("Paragraph count not included")
	}
	for _, p := range paras {
		if !strings.Contains(result, p) {
			t.Errorf("Paragraph %q not found in user prompt", p)
		}
	}
}

func TestBuildUserPrompt_NoTitle(t *testing.T) {
	result := BuildUserPrompt("", []string{"只有内容"})
	if strings.Contains(result, "ชื่อตอน:") {
		t.Error("Should not include title header when title is empty")
	}
}

func TestParseTranslationOutput_BasicParagraphs(t *testing.T) {
	input := "ย่อหน้าที่หนึ่ง\n\nย่อหน้าที่สอง\n\nย่อหน้าที่สาม"
	title, paras := ParseTranslationOutput(input)

	if title != "" {
		t.Errorf("Expected no title, got %q", title)
	}
	if len(paras) != 3 {
		t.Errorf("Expected 3 paragraphs, got %d", len(paras))
	}
}

func TestParseTranslationOutput_WithTitle(t *testing.T) {
	input := "ตอนที่ 1 จุดเริ่มต้นใหม่\n\nเนื้อหาย่อหน้าแรก\n\nเนื้อหาย่อหน้าที่สอง"
	title, paras := ParseTranslationOutput(input)

	if title != "ตอนที่ 1 จุดเริ่มต้นใหม่" {
		t.Errorf("Title = %q, want 'ตอนที่ 1 จุดเริ่มต้นใหม่'", title)
	}
	if len(paras) != 2 {
		t.Errorf("Expected 2 paragraphs (title extracted), got %d", len(paras))
	}
}

func TestParseTranslationOutput_StripThinkTags(t *testing.T) {
	input := "<think>This is my reasoning about the translation...</think>\nย่อหน้าแปลแล้ว"
	_, paras := ParseTranslationOutput(input)

	for _, p := range paras {
		if strings.Contains(p, "think") || strings.Contains(p, "reasoning") {
			t.Errorf("Think tag content leaked: %q", p)
		}
	}
	if len(paras) < 1 {
		t.Error("Expected at least 1 paragraph after stripping think tags")
	}
}

func TestParseTranslationOutput_StripUnclosedThinkTag(t *testing.T) {
	input := "<think>This reasoning never closes\nย่อหน้าจริง"
	_, paras := ParseTranslationOutput(input)

	if len(paras) != 0 {
		t.Error("Unfinished reasoning must not become translated content")
	}
}

func TestParseTranslationOutput_StripMarkdownFences(t *testing.T) {
	input := "```markdown\nย่อหน้าในโค้ดบล็อก\n\nย่อหน้าที่สอง\n```"
	_, paras := ParseTranslationOutput(input)

	if len(paras) != 2 {
		t.Errorf("Expected 2 paragraphs from markdown block, got %d", len(paras))
	}
	for _, p := range paras {
		if strings.Contains(p, "```") {
			t.Error("Markdown fence leaked into output")
		}
	}
}

func TestParseTranslationOutput_FilterAIPreamble(t *testing.T) {
	preambles := []string{
		"นี่คือคำแปลของเนื้อหา",
		"ต่อไปนี้คือคำแปล",
		"[เนื้อหาที่แปลแล้ว]",
	}
	for _, pre := range preambles {
		input := fmt.Sprintf("%s\n\nเนื้อหาจริง", pre)
		_, paras := ParseTranslationOutput(input)

		for _, p := range paras {
			if strings.HasPrefix(p, pre) {
				t.Errorf("AI preamble not filtered: %q", pre)
			}
		}
	}
}

func TestParseTranslationOutput_EmptyInput(t *testing.T) {
	title, paras := ParseTranslationOutput("")
	if title != "" {
		t.Errorf("Expected empty title, got %q", title)
	}
	if len(paras) != 0 {
		t.Errorf("Expected 0 paragraphs, got %d", len(paras))
	}
}

func TestParseTranslationOutput_PreservesHanziForQA(t *testing.T) {
	input := "เนื้อหาที่มี获得ตัวจีนหลุด"
	_, paras := ParseTranslationOutput(input)

	if len(paras) != 1 || !HasHanzi(paras[0]) {
		t.Fatalf("parser must preserve language leaks for downstream QA, got %#v", paras)
	}
}

func TestBuildSystemPrompt_WithStyleRules(t *testing.T) {
	rules := "[punctuation]\n- Use em-dash for missing numbers"
	prompt := BuildSystemPrompt(nil, "", "system", rules)
	if !strings.Contains(prompt, "Style Rules") {
		t.Error("Style rules section header not found in prompt")
	}
	if !strings.Contains(prompt, "Use em-dash for missing numbers") {
		t.Error("Style rule content not injected into prompt")
	}
}

func TestBuildSystemPrompt_EmptyStyleRules(t *testing.T) {
	prompt := BuildSystemPrompt(nil, "", "system", "   ")
	if strings.Contains(prompt, "Style Rules") {
		t.Error("Empty style rules should not inject a section")
	}
}
