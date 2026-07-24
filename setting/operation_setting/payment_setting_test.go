package operation_setting

import "testing"

func TestGetExternalPurchaseLinksFiltersUnsafeValues(t *testing.T) {
	original := paymentSetting.ExternalPurchaseLinks
	t.Cleanup(func() {
		paymentSetting.ExternalPurchaseLinks = original
	})

	paymentSetting.ExternalPurchaseLinks = map[int]string{
		10: "https://pay.example.com/item/ten",
		30: " https://pay.example.com/item/thirty ",
		50: "http://pay.example.com/item/fifty",
		0:  "https://pay.example.com/item/zero",
	}

	got := GetExternalPurchaseLinks()
	if len(got) != 2 {
		t.Fatalf("expected 2 safe links, got %d", len(got))
	}
	if got[10] != "https://pay.example.com/item/ten" {
		t.Fatalf("unexpected 10 USD link: %q", got[10])
	}
	if got[30] != "https://pay.example.com/item/thirty" {
		t.Fatalf("unexpected 30 USD link: %q", got[30])
	}
	if _, exists := got[50]; exists {
		t.Fatal("http link must not be exposed")
	}
	if _, exists := got[0]; exists {
		t.Fatal("non-positive amount must not be exposed")
	}
}
