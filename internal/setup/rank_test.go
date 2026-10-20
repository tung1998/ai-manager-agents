package setup_test

import (
	"testing"

	"bitbucket.org/senprints/agent-office/internal/setup"
)

func TestRankPicksMatchingCatalogSkills(t *testing.T) {
	cands := []setup.Candidate{
		{Tool: setup.Tool{Name: "django-orm", Description: "Python web models"}, Keys: []string{"backend", "python"}},
		{Tool: setup.Tool{Name: "nuxt-seo", Description: "Meta tags for Vue apps"}, Keys: []string{"frontend"}},
		{Tool: setup.Tool{Name: "tailwind-tips", Description: "Utility CSS"}, Keys: []string{"css", "tailwind"}},
		{Tool: setup.Tool{Name: "vue-testing", Description: "Test Vue components with nuxt and vitest"}},
	}
	got := setup.Rank("Project: shop\nFramework: Nuxt (Vue)\nStyles: Tailwind", cands)
	if len(got) != 3 || got[0].Name != "nuxt-seo" {
		t.Fatalf("rank = %+v", got)
	}
	for _, g := range got {
		if g.Name == "django-orm" {
			t.Fatalf("unrelated skill kept: %+v", got)
		}
	}
	if setup.Rank("", cands) != nil {
		t.Fatal("no hint should rank nothing")
	}
}
