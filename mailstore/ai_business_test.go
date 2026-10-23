package mailstore

import (
	"context"
	"testing"
)

func TestAIBusinessProfilePersistenceAndTemplateOwnership(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "business-test", "Business test", "unused")
	if err != nil {
		t.Fatal(err)
	}
	owner := user.ID
	template, err := s.SaveDocumentTemplate(ctx, DocumentTemplateRecord{OwnerID: owner, Type: "quotation", Name: "A quotation", HTML: "canvas"})
	if err != nil {
		t.Fatal(err)
	}
	p := AIBusinessProfile{Company: " Alice Ltd ", Brands: []string{" A ", "A", "B"}, BrandMode: "restrict", QuotationTemplateID: template.ID}
	a, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: owner, Name: "Sales", Prompt: "sales", BusinessProfile: p})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAIAgent(ctx, owner, a.ID)
	if err != nil || got.BusinessProfile.Company != "Alice Ltd" || len(got.BusinessProfile.Brands) != 2 || got.BusinessProfile.QuotationTemplateID != template.ID {
		t.Fatalf("%+v %v", got, err)
	}
	for _, profile := range []AIBusinessProfile{
		{QuotationTemplateID: template.ID, ContractTemplateID: template.ID},
		{BrandMode: "restrict"},
		{BrandMode: "invented"},
		{Brands: []string{"\n"}},
	} {
		if _, err = s.ValidateAIBusinessProfile(ctx, owner, profile); err == nil {
			t.Fatalf("invalid profile accepted: %+v", profile)
		}
	}
	if _, err = s.ValidateAIBusinessProfile(ctx, "bob", p); err == nil {
		t.Fatal("foreign template accepted")
	}
	if _, err = s.ValidateAIBusinessProfile(ctx, owner, AIBusinessProfile{QuotationTemplateID: "default-quotation", ContractTemplateID: "default-contract"}); err != nil {
		t.Fatal(err)
	}
	// Migrate an existing agent table in place, twice, without losing its prompt.
	if _, err = s.db.ExecContext(ctx, `ALTER TABLE ai_agents DROP COLUMN business_profile_json`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = s.migrateAIBusiness(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.GetAIAgent(ctx, owner, a.ID)
	if err != nil || got.Prompt != "sales" || got.BusinessProfile.BrandMode != "prefer" {
		t.Fatalf("upgrade lost legacy data: %+v %v", got, err)
	}
}
