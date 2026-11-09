package model

import (
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InvitationCommission is an immutable receipt for one committed purchase.
// Source IDs refer to database rows; redemption secrets are never stored here.
type InvitationCommission struct {
	Id          int    `json:"id"`
	Source      string `json:"source" gorm:"type:varchar(32);not null;uniqueIndex:idx_invitation_source"`
	SourceId    int    `json:"source_id" gorm:"not null;uniqueIndex:idx_invitation_source"`
	InviteeId   int    `json:"invitee_id" gorm:"not null;index"`
	InviterId   int    `json:"inviter_id" gorm:"not null;index"`
	BaseQuota   int64  `json:"base_quota" gorm:"not null"`
	RateBPS     int    `json:"rate_bps" gorm:"not null"`
	RewardQuota int64  `json:"reward_quota" gorm:"not null"`
	CreatedAt   int64  `json:"created_at" gorm:"not null;index"`
}

func normalizedInvitationOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return "https://" + strings.ToLower(u.Host)
}

func GetInvitationPolicy() operation_setting.InvitationSetting {
	common.OptionMapRWMutex.RLock()
	p := *operation_setting.GetInvitationSetting()
	origin := system_setting.ServerAddress
	common.OptionMapRWMutex.RUnlock()
	p.Enabled = p.Enabled && p.EnabledAt > 0 && p.EnabledAt <= common.GetTimestamp() &&
		normalizedInvitationOrigin(p.SiteOrigin) != "" &&
		normalizedInvitationOrigin(p.SiteOrigin) == normalizedInvitationOrigin(origin) &&
		p.BalanceRateBPS >= 0 && p.BalanceRateBPS <= 10000 &&
		p.SubscriptionRateBPS >= 0 && p.SubscriptionRateBPS <= 10000
	return p
}

func IsInvitationEnabled() bool { return GetInvitationPolicy().Enabled }

// UpdateInvitationPolicy commits the full policy together, then publishes one
// in-process snapshot. Saving several independent switches must not expose a
// partially configured commission policy to a payment callback.
func UpdateInvitationPolicy(p operation_setting.InvitationSetting) error {
	if p.BalanceRateBPS < 0 || p.BalanceRateBPS > 10000 || p.SubscriptionRateBPS < 0 || p.SubscriptionRateBPS > 10000 || p.EnabledAt < 0 || p.MinRedemptionIssuedAt < 0 {
		return errors.New("无效的邀请奖励配置")
	}
	common.OptionMapRWMutex.RLock()
	origin := system_setting.ServerAddress
	common.OptionMapRWMutex.RUnlock()
	if p.Enabled && (p.EnabledAt <= 0 || normalizedInvitationOrigin(p.SiteOrigin) == "" || normalizedInvitationOrigin(p.SiteOrigin) != normalizedInvitationOrigin(origin)) {
		return errors.New("邀请站点与当前站点不一致，或未设置生效时间")
	}
	values := map[string]string{
		"invitation_setting.enabled":                  strconv.FormatBool(p.Enabled),
		"invitation_setting.site_origin":              p.SiteOrigin,
		"invitation_setting.balance_rate_bps":         strconv.Itoa(p.BalanceRateBPS),
		"invitation_setting.subscription_rate_bps":    strconv.Itoa(p.SubscriptionRateBPS),
		"invitation_setting.enabled_at":               strconv.FormatInt(p.EnabledAt, 10),
		"invitation_setting.min_redemption_issued_at": strconv.FormatInt(p.MinRedemptionIssuedAt, 10),
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		for key, value := range values {
			if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&Option{Key: key, Value: value}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	*operation_setting.GetInvitationSetting() = p
	for key, value := range values {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	return nil
}

func ValidateInvitationOption(key, value string) error {
	switch key {
	case "invitation_setting.balance_rate_bps", "invitation_setting.subscription_rate_bps":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 10000 {
			return errors.New("返佣比例必须在 0% 到 100% 之间")
		}
	case "invitation_setting.enabled_at", "invitation_setting.min_redemption_issued_at":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 {
			return errors.New("无效的邀请生效时间")
		}
	case "invitation_setting.site_origin":
		if value != "" && normalizedInvitationOrigin(value) == "" {
			return errors.New("邀请站点地址必须是 HTTPS 站点地址")
		}
	case "invitation_setting.enabled":
		if value != "true" && value != "false" {
			return errors.New("无效的邀请开关")
		}
		if value == "true" {
			common.OptionMapRWMutex.RLock()
			p := *operation_setting.GetInvitationSetting()
			origin := system_setting.ServerAddress
			common.OptionMapRWMutex.RUnlock()
			if p.EnabledAt <= 0 || normalizedInvitationOrigin(p.SiteOrigin) == "" || normalizedInvitationOrigin(p.SiteOrigin) != normalizedInvitationOrigin(origin) {
				return errors.New("请先配置邀请站点和生效时间")
			}
		}
	}
	return nil
}

func invitationUserCreateTx(tx *gorm.DB, user *User, inviterId int) error {
	user.InviterId = 0
	if IsInvitationEnabled() && inviterId > 0 && inviterId != user.Id {
		var inviter User
		if err := tx.Select("id").Where("id = ? AND status = ?", inviterId, common.UserStatusEnabled).First(&inviter).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		} else {
			user.InviterId = inviter.Id
		}
	}
	if err := tx.Create(user).Error; err != nil {
		return err
	}
	if user.InviterId > 0 {
		return tx.Model(&User{}).Where("id = ?", user.InviterId).Update("aff_count", gorm.Expr("aff_count + 1")).Error
	}
	return nil
}

func GetInvitationCount(userId int) (int64, error) {
	var count int64
	err := DB.Unscoped().Model(&User{}).Where("inviter_id = ?", userId).Count(&count).Error
	return count, err
}

// creditInvitationTx shares the purchase transaction: either both credits and
// the receipt commit, or none of them do. The source row must already be locked.
func creditInvitationTx(tx *gorm.DB, p operation_setting.InvitationSetting, source string, sourceId, inviteeId int, base int64, rate int, completedAt int64) error {
	if !p.Enabled || completedAt < p.EnabledAt || base <= 0 || rate <= 0 {
		return nil
	}
	if tx == nil || sourceId <= 0 || rate > 10000 {
		return errors.New("invalid invitation purchase")
	}
	var invitee User
	if err := tx.Select("id", "inviter_id").First(&invitee, inviteeId).Error; err != nil {
		return err
	}
	if invitee.InviterId <= 0 || invitee.InviterId == invitee.Id {
		return nil
	}
	var inviter User
	if err := tx.Select("id").Where("id = ? AND status = ?", invitee.InviterId, common.UserStatusEnabled).First(&inviter).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	reward := decimal.NewFromInt(base).Mul(decimal.NewFromInt(int64(rate))).Div(decimal.NewFromInt(10000)).IntPart()
	if reward <= 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&InvitationCommission{}).Where("source = ? AND source_id = ?", source, sourceId).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	receipt := InvitationCommission{Source: source, SourceId: sourceId, InviteeId: inviteeId, InviterId: inviter.Id, BaseQuota: base, RateBPS: rate, RewardQuota: reward, CreatedAt: completedAt}
	if err := tx.Create(&receipt).Error; err != nil {
		return err
	}
	r := tx.Model(&User{}).Where("id = ? AND status = ?", inviter.Id, common.UserStatusEnabled).Updates(map[string]interface{}{
		"aff_quota": gorm.Expr("aff_quota + ?", reward), "aff_history": gorm.Expr("aff_history + ?", reward),
	})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errors.New("invitation recipient changed")
	}
	return nil
}

func creditTopUpInvitationTx(tx *gorm.DB, topUp *TopUp, quota int64) error {
	p := GetInvitationPolicy()
	return creditInvitationTx(tx, p, "topup", topUp.Id, topUp.UserId, quota, p.BalanceRateBPS, topUp.CompleteTime)
}

// InvitationSubscriptionQuota uses the advertised whole-card allowance. A
// 28-day card with four 7-day budgets contributes four times its period budget.
func InvitationSubscriptionQuota(plan *SubscriptionPlan, sub *UserSubscription) (int64, error) {
	if plan == nil || sub == nil || plan.TotalAmount <= 0 {
		return 0, nil
	}
	seconds := sub.EndTime - sub.StartTime
	if seconds != 7*86400 && (seconds < 28*86400 || seconds > 31*86400) {
		return 0, nil
	}
	periods := int64(1)
	switch NormalizeResetPeriod(plan.QuotaResetPeriod) {
	case SubscriptionResetDaily:
		periods = (seconds + 86400 - 1) / 86400
	case SubscriptionResetWeekly:
		periods = (seconds + 7*86400 - 1) / (7 * 86400)
	case SubscriptionResetCustom:
		if plan.QuotaResetCustomSeconds <= 0 {
			return 0, errors.New("invalid subscription reset interval")
		}
		periods = (seconds + plan.QuotaResetCustomSeconds - 1) / plan.QuotaResetCustomSeconds
	}
	if periods <= 0 || plan.TotalAmount > math.MaxInt64/periods {
		return 0, errors.New("subscription invitation allowance overflow")
	}
	return plan.TotalAmount * periods, nil
}

func creditSubscriptionInvitationTx(tx *gorm.DB, source string, sourceId, userId int, plan *SubscriptionPlan, sub *UserSubscription) error {
	p := GetInvitationPolicy()
	return creditSubscriptionInvitationWithPolicyTx(tx, p, source, sourceId, userId, plan, sub)
}

func creditSubscriptionInvitationWithPolicyTx(tx *gorm.DB, p operation_setting.InvitationSetting, source string, sourceId, userId int, plan *SubscriptionPlan, sub *UserSubscription) error {
	if !p.Enabled || p.SubscriptionRateBPS <= 0 {
		return nil
	}
	base, err := InvitationSubscriptionQuota(plan, sub)
	if err != nil {
		return err
	}
	return creditInvitationTx(tx, p, source, sourceId, userId, base, p.SubscriptionRateBPS, sub.CreatedAt)
}

// CompleteBalanceTopUp covers verified ePay callbacks in one transaction.
func CompleteBalanceTopUp(tradeNo, expectedPaymentMethod string) (*TopUp, int, bool, error) {
	var topUp TopUp
	var quota int
	var applied bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if topUp.PaymentMethod != expectedPaymentMethod {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		quota = int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if quota <= 0 {
			return errors.New("无效的充值额度")
		}
		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quota)).Error; err != nil {
			return err
		}
		topUp.Status, topUp.CompleteTime = common.TopUpStatusSuccess, common.GetTimestamp()
		if err := tx.Save(&topUp).Error; err != nil {
			return err
		}
		if err := creditTopUpInvitationTx(tx, &topUp, int64(quota)); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied {
		_ = invalidateUserCache(topUp.UserId)
	}
	return &topUp, quota, applied, err
}

func invitationDisabledError() error { return errors.New("本站未开启邀请奖励") }
