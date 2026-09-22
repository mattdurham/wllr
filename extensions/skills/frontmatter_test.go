package main

import "testing"

func TestParseFrontmatter(t *testing.T) {
	content := `---
name: bob:work
description: "Plan and execute work"
category: workflow
model: high
thinking: high
user-invocable: true
---

# Body

Steps.`
	fm, body := parseFrontmatter(content)
	if fm == nil {
		t.Fatal("frontmatter not parsed")
	}
	for key, want := range map[string]string{
		"name":        "bob:work",
		"description": "Plan and execute work",
		"model":       "high",
		"thinking":    "high",
	} {
		if got := fm[key]; got != want {
			t.Errorf("frontmatter[%q] = %q, want %q", key, got, want)
		}
	}
	if body != "# Body\n\nSteps." {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	fm, body := parseFrontmatter("# Just a body\n")
	if fm != nil {
		t.Errorf("fm = %v, want nil", fm)
	}
	if body != "# Just a body" {
		t.Errorf("body = %q", body)
	}
}

func TestSkillMetaModelFields(t *testing.T) {
	// The loader maps frontmatter model/thinking onto skillMeta; "default" is a
	// sentinel meaning "do not change the model".
	content := `---
name: s
model: low
thinking: minimal
---
body`
	fm, _ := parseFrontmatter(content)
	meta := skillMeta{Name: "s"}
	if v := fm["model"]; v != "" && v != "default" {
		meta.Model = v
	}
	if v := fm["thinking"]; v != "" {
		meta.Thinking = v
	}
	if meta.Model != "low" || meta.Thinking != "minimal" {
		t.Fatalf("meta = %+v", meta)
	}
}
