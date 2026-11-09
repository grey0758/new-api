package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEpayInvitationAcknowledgesOnlyCommittedPurchase(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldSQLite, oldMySQL, oldPostgres, oldRedis := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL, common.RedisEnabled
	oldPolicy, oldOrigin := *operation_setting.GetInvitationSetting(), system_setting.ServerAddress
	oldAddress, oldID, oldKey, oldMethods := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayMethods
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL, common.RedisEnabled = oldSQLite, oldMySQL, oldPostgres, oldRedis
		common.OptionMapRWMutex.Lock()
		*operation_setting.GetInvitationSetting(), system_setting.ServerAddress = oldPolicy, oldOrigin
		common.OptionMapRWMutex.Unlock()
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayMethods = oldAddress, oldID, oldKey, oldMethods
	})
	db := setupRedemptionTokenTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.InvitationCommission{}))
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://synthetic-payment.invalid", "synthetic-id", "synthetic-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	common.OptionMapRWMutex.Lock()
	*operation_setting.GetInvitationSetting() = operation_setting.InvitationSetting{
		Enabled: true, SiteOrigin: "https://api.open-codex.com", BalanceRateBPS: 1000, EnabledAt: common.GetTimestamp() - 1,
	}
	system_setting.ServerAddress = "https://api.open-codex.com"
	common.OptionMapRWMutex.Unlock()
	inviter := model.User{Username: "epay-inviter", AffCode: "epay-inviter"}
	require.NoError(t, db.Create(&inviter).Error)
	buyer := model.User{Username: "epay-buyer", AffCode: "epay-buyer", InviterId: inviter.Id}
	require.NoError(t, db.Create(&buyer).Error)
	order := model.TopUp{UserId: buyer.Id, TradeNo: "synthetic-epay-commission", Amount: 10, Money: 10, PaymentMethod: "alipay", Status: common.TopUpStatusPending}
	require.NoError(t, db.Create(&order).Error)

	callback := func() string {
		params := epay.GenerateParams(map[string]string{
			"pid": "synthetic-id", "out_trade_no": order.TradeNo, "trade_no": "synthetic-provider-trade",
			"type": "alipay", "trade_status": "TRADE_SUCCESS", "money": "10.00",
		}, operation_setting.EpayKey)
		form := url.Values{}
		for key, value := range params {
			form.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
		ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		EpayNotify(ctx)
		return recorder.Body.String()
	}

	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_receipt_failure", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.InvitationCommission); ok {
			tx.AddError(errors.New("synthetic receipt failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("synthetic_receipt_failure") })
	require.Equal(t, "fail", callback())
	require.NoError(t, db.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.NoError(t, db.First(&buyer, buyer.Id).Error)
	require.Zero(t, buyer.Quota)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	require.Zero(t, inviter.AffQuota)

	require.NoError(t, db.Callback().Create().Remove("synthetic_receipt_failure"))
	require.Equal(t, "success", callback())
	require.Equal(t, "success", callback())
	require.NoError(t, db.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	require.NoError(t, db.First(&buyer, buyer.Id).Error)
	require.Equal(t, 10*int(common.QuotaPerUnit), buyer.Quota)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	require.Equal(t, int(common.QuotaPerUnit), inviter.AffQuota)
	var count int64
	require.NoError(t, db.Model(&model.InvitationCommission{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
