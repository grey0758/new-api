package model

import "testing"

func TestSubscriptionPlanApplyPricingPolicy_MonthlyCard(t *testing.T) {
	plan := SubscriptionPlan{
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   12.34,
	}

	plan.ApplyPricingPolicy()

	if plan.PriceAmount != SubscriptionMonthlyCardPriceAmount {
		t.Fatalf("expected monthly card price to be %v, got %v", SubscriptionMonthlyCardPriceAmount, plan.PriceAmount)
	}
}

func TestSubscriptionPlanApplyPricingPolicy_NonMonthlyCard(t *testing.T) {
	plan := SubscriptionPlan{
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 3,
		PriceAmount:   88.8,
	}

	plan.ApplyPricingPolicy()

	if plan.PriceAmount != 88.8 {
		t.Fatalf("expected non-monthly card price to stay unchanged, got %v", plan.PriceAmount)
	}
}
