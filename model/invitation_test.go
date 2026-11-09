package model

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func invitationFixture(t *testing.T) (User, User) {
	t.Helper()
	truncateTables(t)
	old := *operation_setting.GetInvitationSetting()
	oldOrigin := system_setting.ServerAddress
	common.OptionMapRWMutex.Lock()
	*operation_setting.GetInvitationSetting() = operation_setting.InvitationSetting{
		Enabled: true, SiteOrigin: "https://api.open-codex.com", BalanceRateBPS: 1000,
		SubscriptionRateBPS: 500, EnabledAt: common.GetTimestamp() - 1,
	}
	system_setting.ServerAddress = "https://api.open-codex.com"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		*operation_setting.GetInvitationSetting() = old
		system_setting.ServerAddress = oldOrigin
		common.OptionMapRWMutex.Unlock()
	})
	inviter := User{Id: 91001, Username: "commission_inviter", AffCode: "test-inviter", Quota: 1000000}
	invitee := User{Id: 91002, Username: "commission_invitee", AffCode: "test-invitee", InviterId: inviter.Id}
	require.NoError(t, DB.Create(&inviter).Error)
	require.NoError(t, DB.Create(&invitee).Error)
	return inviter, invitee
}

func invitationTotals(t *testing.T, id int, remaining, historical int) {
	t.Helper()
	var u User
	require.NoError(t, DB.First(&u, id).Error)
	require.Equal(t, remaining, u.AffQuota)
	require.Equal(t, historical, u.AffHistoryQuota)
}

func TestInvitationRegistrationTracksWithoutFixedGrant(t *testing.T) {
	inviter, _ := invitationFixture(t)
	oldA, oldB := common.QuotaForInviter, common.QuotaForInvitee
	common.QuotaForInviter, common.QuotaForInvitee = 500000, 500000
	t.Cleanup(func() { common.QuotaForInviter, common.QuotaForInvitee = oldA, oldB })
	u := User{Id: 91003, Username: "new_referral"}
	require.NoError(t, u.Insert(inviter.Id))
	require.Equal(t, inviter.Id, u.InviterId)
	var got User
	require.NoError(t, DB.First(&got, inviter.Id).Error)
	require.Equal(t, 1, got.AffCount)
	require.Equal(t, 0, got.AffQuota)
	got = User{}
	require.NoError(t, DB.First(&got, u.Id).Error)
	require.Equal(t, common.QuotaForNewUser, got.Quota)
}

func TestInvitationOAuthRegistrationBindingAndRollback(t *testing.T) {
	inviter, _ := invitationFixture(t)
	u := User{Id: 91004, Username: "oauth_referral"}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return u.InsertWithTx(tx, inviter.Id) }))
	require.Equal(t, inviter.Id, u.InviterId)
	v := User{Id: 91005, Username: "rollback_referral"}
	require.Error(t, DB.Transaction(func(tx *gorm.DB) error {
		if err := v.InsertWithTx(tx, inviter.Id); err != nil {
			return err
		}
		return errors.New("binding failed")
	}))
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("id = ?", v.Id).Count(&count).Error)
	require.Zero(t, count)
	var got User
	require.NoError(t, DB.First(&got, inviter.Id).Error)
	require.Equal(t, 1, got.AffCount)
}

func TestInvitationOtherSiteBlocksBindingRewardsAndTransfer(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	system_setting.ServerAddress = "https://api.opencodex.uk"
	require.False(t, IsInvitationEnabled())
	_, err := GetUserIdByAffCode(inviter.AffCode)
	require.Error(t, err)
	u := User{Id: 91006, Username: "other_site", InviterId: inviter.Id}
	require.NoError(t, u.Insert(inviter.Id))
	require.Zero(t, u.InviterId)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return creditInvitationTx(tx, GetInvitationPolicy(), "topup", 1, invitee.Id, 5000000, 1000, common.GetTimestamp())
	}))
	invitationTotals(t, inviter.Id, 0, 0)
	require.Error(t, inviter.TransferAffQuotaToQuota(500000))
}

func TestInvitationBalanceRedemptionTenPercentExactlyOnce(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	code := Redemption{Key: "synthetic-balance-code", Name: "test", Quota: 50000000}
	require.NoError(t, code.Insert())
	got, err := Redeem(code.Key, invitee.Id)
	require.NoError(t, err)
	require.Equal(t, 50000000, got.Quota)
	_, err = Redeem(code.Key, invitee.Id)
	require.Error(t, err)
	invitationTotals(t, inviter.Id, 5000000, 5000000)
	var receipts []InvitationCommission
	require.NoError(t, DB.Find(&receipts).Error)
	require.Len(t, receipts, 1)
	require.EqualValues(t, 50000000, receipts[0].BaseQuota)
	require.Equal(t, "balance_redemption", receipts[0].Source)
}

func TestInvitationWeeklyAndWholeMonthlyRedemption(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		days                       int
		budget, interval, expected int64
	}{
		{"weekly350", 7, 175000000, 0, 8750000},
		{"weekly700", 7, 350000000, 0, 17500000},
		{"monthly700", 28, 87500000, 604800, 17500000},
		{"monthly1400", 28, 175000000, 604800, 35000000},
		{"monthly2800", 28, 350000000, 604800, 70000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inviter, invitee := invitationFixture(t)
			reset := SubscriptionResetNever
			if tc.interval > 0 {
				reset = SubscriptionResetCustom
			}
			p := SubscriptionPlan{Id: 92001, Title: tc.name, Enabled: true, DurationUnit: "day", DurationValue: tc.days, TotalAmount: tc.budget, QuotaResetPeriod: reset, QuotaResetCustomSeconds: tc.interval}
			require.NoError(t, DB.Create(&p).Error)
			InvalidateSubscriptionPlanCache(p.Id)
			code := Redemption{Key: "synthetic-subscription-code", Type: RedemptionTypeSubscription, SubscriptionPlanId: p.Id}
			require.NoError(t, code.Insert())
			got, err := Redeem(code.Key, invitee.Id)
			require.NoError(t, err)
			require.Equal(t, tc.budget, got.Subscription.AmountTotal)
			invitationTotals(t, inviter.Id, int(tc.expected), int(tc.expected))
		})
	}
}

func TestInvitationSubscriptionOrderRepeatedCallback(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	p := SubscriptionPlan{Id: 92002, Title: "monthly", Enabled: true, DurationUnit: "day", DurationValue: 28, TotalAmount: 87500000, QuotaResetPeriod: "custom", QuotaResetCustomSeconds: 604800}
	require.NoError(t, DB.Create(&p).Error)
	InvalidateSubscriptionPlanCache(p.Id)
	order := SubscriptionOrder{UserId: invitee.Id, PlanId: p.Id, TradeNo: "synthetic-sub-order", Money: 65, PaymentMethod: PaymentMethodStripe, Status: common.TopUpStatusPending}
	require.NoError(t, DB.Create(&order).Error)
	for i := 0; i < 2; i++ {
		require.NoError(t, CompleteSubscriptionOrder(order.TradeNo, "", order.PaymentMethod))
	}
	invitationTotals(t, inviter.Id, 17500000, 17500000)
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestInvitationTopUpProvidersAndDuplicateCallbacks(t *testing.T) {
	for _, method := range []string{PaymentMethodStripe, PaymentMethodCreem, PaymentMethodWaffo, PaymentMethodWaffoPancake, "alipay", PaymentMethodWxDaEpayWxpay, "manual"} {
		t.Run(method, func(t *testing.T) {
			inviter, invitee := invitationFixture(t)
			paymentMethod, amount := method, int64(10)
			if method == PaymentMethodCreem {
				amount = 5000000
			}
			if method == "manual" {
				paymentMethod = "alipay"
			}
			order := TopUp{UserId: invitee.Id, Amount: amount, Money: 10, TradeNo: "synthetic-topup", PaymentMethod: paymentMethod, Status: common.TopUpStatusPending}
			require.NoError(t, order.Insert())
			for i := 0; i < 2; i++ {
				var err error
				switch method {
				case PaymentMethodStripe:
					err = Recharge(order.TradeNo, "", "")
				case PaymentMethodCreem:
					err = RechargeCreem(order.TradeNo, "", "", "")
				case PaymentMethodWaffo:
					err = RechargeWaffo(order.TradeNo, "")
				case PaymentMethodWaffoPancake:
					err = RechargeWaffoPancake(order.TradeNo)
				case "manual":
					err = ManualCompleteTopUp(order.TradeNo, "")
				default:
					_, _, _, err = CompleteBalanceTopUp(order.TradeNo, method)
				}
				require.NoError(t, err)
			}
			invitationTotals(t, inviter.Id, 500000, 500000)
			var got User
			require.NoError(t, DB.First(&got, invitee.Id).Error)
			require.Equal(t, 5000000, got.Quota)
		})
	}
}

func TestInvitationReceiptFailureRollsBackPurchase(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	order := TopUp{UserId: invitee.Id, Amount: 10, TradeNo: "rollback-purchase", PaymentMethod: "alipay", Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("fail_invitation_receipt", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*InvitationCommission); ok {
			tx.AddError(errors.New("synthetic ledger write failure"))
		}
	}))
	t.Cleanup(func() { DB.Callback().Create().Remove("fail_invitation_receipt") })
	_, _, _, err := CompleteBalanceTopUp(order.TradeNo, order.PaymentMethod)
	require.Error(t, err)
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	var u User
	require.NoError(t, DB.First(&u, invitee.Id).Error)
	require.Zero(t, u.Quota)
	invitationTotals(t, inviter.Id, 0, 0)
}

func TestInvitationConcurrentPurchasesAndTransfers(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		order := TopUp{UserId: invitee.Id, Amount: 10, TradeNo: fmt.Sprintf("parallel-%d", i), PaymentMethod: "alipay", Status: common.TopUpStatusPending}
		require.NoError(t, order.Insert())
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _, err := CompleteBalanceTopUp(order.TradeNo, "alipay"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	invitationTotals(t, inviter.Id, 6000000, 6000000)
	var success int
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if inviter.TransferAffQuotaToQuota(500000) == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 12, success)
	invitationTotals(t, inviter.Id, 0, 6000000)
	var got User
	require.NoError(t, DB.First(&got, inviter.Id).Error)
	require.Equal(t, 7000000, got.Quota)
}

func TestInvitationEligibilityAndRounding(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	for i, tc := range []struct {
		quota    int64
		rate     int
		at       int64
		expected int
	}{
		{19, 1000, common.GetTimestamp(), 1},
		{9, 1000, common.GetTimestamp(), 0},
		{5000000, 1000, 1, 0},
		{0, 1000, common.GetTimestamp(), 0},
	} {
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			return creditInvitationTx(tx, GetInvitationPolicy(), "topup", i+1, invitee.Id, tc.quota, tc.rate, tc.at)
		}))
		if tc.expected == 1 {
			invitationTotals(t, inviter.Id, 1, 1)
		}
	}
	invitationTotals(t, inviter.Id, 1, 1)
	_, err := InvitationSubscriptionQuota(&SubscriptionPlan{TotalAmount: 100}, &UserSubscription{StartTime: 1, EndTime: 86401})
	require.NoError(t, err)
}

func TestInvitationConcurrentDuplicateCallbackPaysOnce(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	order := TopUp{UserId: invitee.Id, Amount: 10, TradeNo: "duplicate-parallel", PaymentMethod: "alipay", Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _, err := CompleteBalanceTopUp(order.TradeNo, "alipay"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	invitationTotals(t, inviter.Id, 500000, 500000)
	var buyer User
	require.NoError(t, DB.First(&buyer, invitee.Id).Error)
	require.Equal(t, 5000000, buyer.Quota)
}

func TestInvitationNoSelfRewardOrDisabledRecipient(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	require.NoError(t, DB.Model(&invitee).Update("inviter_id", invitee.Id).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return creditInvitationTx(tx, GetInvitationPolicy(), "topup", 1, invitee.Id, 5000000, 1000, common.GetTimestamp())
	}))
	invitationTotals(t, invitee.Id, 0, 0)
	require.NoError(t, DB.Model(&invitee).Update("inviter_id", inviter.Id).Error)
	require.NoError(t, DB.Model(&inviter).Update("status", common.UserStatusDisabled).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return creditInvitationTx(tx, GetInvitationPolicy(), "topup", 2, invitee.Id, 5000000, 1000, common.GetTimestamp())
	}))
	invitationTotals(t, inviter.Id, 0, 0)
}

func TestInvitationNewCodeCutoffAndNoAdministrativeGift(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	operation_setting.GetInvitationSetting().MinRedemptionIssuedAt = common.GetTimestamp()
	old := Redemption{Key: "old-synthetic-code", Quota: 5000000, CreatedTime: 1}
	require.NoError(t, old.Insert())
	_, err := Redeem(old.Key, invitee.Id)
	require.NoError(t, err)
	invitationTotals(t, inviter.Id, 0, 0)
	p := SubscriptionPlan{Id: 92003, Title: "weekly gift", Enabled: true, DurationUnit: "day", DurationValue: 7, TotalAmount: 175000000}
	require.NoError(t, DB.Create(&p).Error)
	InvalidateSubscriptionPlanCache(p.Id)
	_, err = AdminBindSubscription(invitee.Id, p.Id, "synthetic gift")
	require.NoError(t, err)
	invitationTotals(t, inviter.Id, 0, 0)
}

func TestInvitationPolicyValidationAndCommissionReceiptDedup(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	require.Error(t, ValidateInvitationOption("invitation_setting.balance_rate_bps", "-1"))
	require.Error(t, ValidateInvitationOption("invitation_setting.subscription_rate_bps", "10001"))
	require.Error(t, ValidateInvitationOption("invitation_setting.site_origin", "https://evil.test/path"))
	require.NoError(t, ValidateInvitationOption("invitation_setting.site_origin", "https://api.open-codex.com"))
	for i := 0; i < 2; i++ {
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			return creditInvitationTx(tx, GetInvitationPolicy(), "topup", 1, invitee.Id, 5000000, 1000, common.GetTimestamp())
		}))
	}
	invitationTotals(t, inviter.Id, 500000, 500000)
}

func TestInvitationPolicySaveRollbackAndReload(t *testing.T) {
	invitationFixture(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	t.Cleanup(func() { DB.Where("key LIKE ?", "invitation_setting.%").Delete(&Option{}) })
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldMap
		common.OptionMapRWMutex.Unlock()
	})
	p := GetInvitationPolicy()
	p.SubscriptionRateBPS = 500
	require.NoError(t, UpdateInvitationPolicy(p))
	require.Equal(t, p, GetInvitationPolicy())
	require.Error(t, UpdateOption("invitation_setting.subscription_rate_bps", "900"))
	require.Equal(t, p, GetInvitationPolicy())
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("fail_invitation_policy", func(tx *gorm.DB) {
		if option, ok := tx.Statement.Dest.(*Option); ok && option.Key == "invitation_setting.subscription_rate_bps" {
			tx.AddError(errors.New("synthetic option failure"))
		}
	}))
	t.Cleanup(func() { DB.Callback().Create().Remove("fail_invitation_policy") })
	next := p
	next.SubscriptionRateBPS, next.BalanceRateBPS = 900, 1500
	require.Error(t, UpdateInvitationPolicy(next))
	require.Equal(t, p, GetInvitationPolicy())
	require.NoError(t, DB.Callback().Create().Remove("fail_invitation_policy"))
	loadOptionsFromDatabase()
	require.Equal(t, p, GetInvitationPolicy())
	next.SiteOrigin = "https://other-site.invalid"
	require.Error(t, UpdateInvitationPolicy(next))
}

func TestInvitationAccumulationBeyond32Bit(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	prior := int(math.MaxInt32 - 1)
	require.NoError(t, DB.Model(&inviter).Updates(map[string]interface{}{"aff_quota": prior, "aff_history": prior}).Error)
	order := TopUp{UserId: invitee.Id, Amount: 10, TradeNo: "large-aff-history", PaymentMethod: "alipay", Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())
	_, _, _, err := CompleteBalanceTopUp(order.TradeNo, order.PaymentMethod)
	require.NoError(t, err)
	invitationTotals(t, inviter.Id, prior+500000, prior+500000)
	require.NoError(t, inviter.TransferAffQuotaToQuota(500000))
	invitationTotals(t, inviter.Id, prior, prior+500000)
}

func TestInvitationLaunchPolicyOldUnusedCodesAndNoBackfill(t *testing.T) {
	inviter, invitee := invitationFixture(t)
	p := operation_setting.GetInvitationSetting()
	p.EnabledAt = common.GetTimestamp() - 1
	p.MinRedemptionIssuedAt = 0
	oldCode := Redemption{Key: "old-unused-before-launch", Quota: 5000000, CreatedTime: 1}
	require.NoError(t, oldCode.Insert())
	_, err := Redeem(oldCode.Key, invitee.Id)
	require.NoError(t, err)
	invitationTotals(t, inviter.Id, 500000, 500000)
	plan := SubscriptionPlan{Id: 92004, Title: "old monthly code", Enabled: true, DurationUnit: "day", DurationValue: 28, TotalAmount: 87500000, QuotaResetPeriod: "custom", QuotaResetCustomSeconds: 604800}
	require.NoError(t, DB.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	oldSubscription := Redemption{Key: "old-unused-monthly-before-launch", Type: RedemptionTypeSubscription, SubscriptionPlanId: plan.Id, CreatedTime: 1}
	require.NoError(t, oldSubscription.Insert())
	_, err = Redeem(oldSubscription.Key, invitee.Id)
	require.NoError(t, err)
	invitationTotals(t, inviter.Id, 18000000, 18000000)

	completed := TopUp{UserId: invitee.Id, TradeNo: "completed-before-launch", Amount: 10, PaymentMethod: "alipay", Status: common.TopUpStatusSuccess, CompleteTime: 1}
	require.NoError(t, completed.Insert())
	_, _, applied, err := CompleteBalanceTopUp(completed.TradeNo, completed.PaymentMethod)
	require.NoError(t, err)
	require.False(t, applied)
	used := Redemption{Key: "used-before-launch", Quota: 5000000, Status: common.RedemptionCodeStatusUsed, CreatedTime: 1, RedeemedTime: 1, UsedUserId: invitee.Id}
	require.NoError(t, DB.Create(&used).Error)
	_, err = Redeem(used.Key, invitee.Id)
	require.Error(t, err)
	invitationTotals(t, inviter.Id, 18000000, 18000000)
	var count int64
	require.NoError(t, DB.Model(&InvitationCommission{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}
