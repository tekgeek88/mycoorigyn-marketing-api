package transactionalemail

import (
	"strings"
	"testing"
)

func TestRenderBrandedHTMLContract(t *testing.T) {
	hostile := `<script>alert("unsafe")</script>`
	url := "https://staging.mycoorigyn.com/signup#access=fixture-token"
	output, err := RenderBrandedHTML(BrandedContent{
		Preheader: "Preview", Eyebrow: "Early access", Heading: "Welcome", Intro: hostile,
		Details:        []BrandedDetail{{Label: "Farm", Value: hostile}},
		Action:         BrandedAction{Label: "Create your farm", URL: url},
		SectionHeading: "Next", Steps: []string{"Create your workspace"}, SecurityNote: "Single-use link.",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"<!doctype html>", "Myco</span><span", "From culture to harvest, all in one place.", "Create your farm", "&lt;script&gt;", "background-color:#06111f", "background-color:#42d8e8"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("branded output missing %q", expected)
		}
	}
	if strings.Contains(output, hostile) || strings.Count(output, "fixture-token") != 3 {
		t.Fatal("dynamic HTML was not escaped or token left intended URL placements")
	}
}

func TestRenderBrandedHTMLRejectsUnsafeURL(t *testing.T) {
	if _, err := RenderBrandedHTML(BrandedContent{Preheader: "p", Heading: "h", Intro: "i", Action: BrandedAction{Label: "go", URL: "javascript:alert(1)"}}); err == nil {
		t.Fatal("unsafe URL accepted")
	}
}
