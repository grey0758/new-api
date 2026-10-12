package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestThirtyDayDailySubscriptionResetsAtLocalMidnight(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	start := time.Date(2026, 10, 12, 14, 30, 0, 0, zone)
	plan := SubscriptionPlan{DurationUnit: "day", DurationValue: 30, QuotaResetPeriod: "daily", PriceAmount: 300, Currency: "CNY"}
	plan.ApplyPricingPolicy()
	require.EqualValues(t, 300, plan.PriceAmount)
	end, err := calcPlanEndTime(start, &plan)
	require.NoError(t, err)
	require.Equal(t, start.Add(30*24*time.Hour).Unix(), end)
	base := start
	for day := 1; day <= 30; day++ {
		next := calcNextResetTime(base, &plan, end)
		require.Equal(t, time.Date(2026, 10, 12+day, 0, 0, 0, 0, zone).Unix(), next)
		base = time.Unix(next, 0).In(zone)
	}
	require.Zero(t, calcNextResetTime(base, &plan, end))
}

func TestSubscriptionCurrencyDefaultsAndValidation(t *testing.T) {
	for input, want := range map[string]string{"": "USD", " usd ": "USD", "cny": "CNY"} {
		got, err := NormalizeSubscriptionCurrency(input)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := NormalizeSubscriptionCurrency("EUR")
	require.Error(t, err)
}
