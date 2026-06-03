package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const wxDaEpayPaymentPrefix = "wxda_epay:"

type WxDaEpayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

func wxDaEpayProviderType(paymentMethod string) string {
	switch paymentMethod {
	case model.PaymentMethodWxDaEpayAlipay:
		return "alipay"
	case model.PaymentMethodWxDaEpayWxpay:
		return "wxpay"
	default:
		return ""
	}
}

func wxDaEpayPaymentMethod(providerType string) string {
	switch providerType {
	case "alipay":
		return model.PaymentMethodWxDaEpayAlipay
	case "wxpay":
		return model.PaymentMethodWxDaEpayWxpay
	default:
		return ""
	}
}

func isWxDaEpayProviderEnabled(providerType string) bool {
	switch providerType {
	case "alipay":
		return setting.WxDaEpayAlipayEnabled
	case "wxpay":
		return setting.WxDaEpayWxpayEnabled
	default:
		return false
	}
}

func RequestWxDaEpay(c *gin.Context) {
	if !isWxDaEpayTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "wxDa 支付未启用或配置不完整"})
		return
	}

	var req WxDaEpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}

	providerType := wxDaEpayProviderType(req.PaymentMethod)
	if providerType == "" || !isWxDaEpayProviderEnabled(providerType) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}

	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	callBackAddress := service.GetCallbackAddress()
	returnUrl, _ := url.Parse(system_setting.ServerAddress + "/console/log")
	notifyUrl, _ := url.Parse(callBackAddress + "/api/user/wxda-epay/notify")
	tradeNo := fmt.Sprintf("WXDAUSR%dNO%s%d", id, common.GetRandomString(6), time.Now().Unix())

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}

	uri, params, err := service.BuildWxDaEpayPurchase(&service.WxDaEpayPurchaseArgs{
		Type:           providerType,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("TUC%d", req.Amount),
		Money:          strconv.FormatFloat(payMoney, 'f', 2, 64),
		NotifyURL:      notifyUrl.String(),
		ReturnURL:      returnUrl.String(),
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("wxDa 支付 拉起支付失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	topUp := &model.TopUp{
		UserId:        id,
		Amount:        amount,
		Money:         payMoney,
		TradeNo:       tradeNo,
		PaymentMethod: req.PaymentMethod,
		CreateTime:    time.Now().Unix(),
		Status:        common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("wxDa 支付 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("wxDa 支付 充值订单创建成功 user_id=%d trade_no=%s payment_method=%s amount=%d money=%.2f uri=%q", id, tradeNo, req.PaymentMethod, req.Amount, payMoney, uri))
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": params, "url": uri})
}

func WxDaEpayNotify(c *gin.Context) {
	if !isWxDaEpayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("wxDa 支付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	params := getWxDaEpayNotifyParams(c)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("wxDa 支付 webhook 收到请求 path=%q client_ip=%s method=%s params=%q", c.Request.RequestURI, c.ClientIP(), c.Request.Method, common.GetJsonString(params)))
	if len(params) == 0 {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	verifyInfo, err := service.VerifyWxDaEpay(params)
	if err != nil || !verifyInfo.VerifyStatus {
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("wxDa 支付 webhook 验签失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		} else {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("wxDa 支付 webhook 验签失败 path=%q client_ip=%s verify_status=false", c.Request.RequestURI, c.ClientIP()))
		}
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if verifyInfo.TradeStatus != service.WxDaEpayTradeSuccess {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("wxDa 支付 webhook 忽略事件 trade_no=%s callback_type=%s trade_status=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("success"))
		return
	}

	if err := completeWxDaEpayTopUp(c, verifyInfo); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("wxDa 支付 充值处理失败 trade_no=%s callback_type=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	_, _ = c.Writer.Write([]byte("success"))
}

func getWxDaEpayNotifyParams(c *gin.Context) map[string]string {
	if c.Request.Method == "POST" {
		if err := c.Request.ParseForm(); err != nil {
			return map[string]string{}
		}
		return lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, key string, _ int) map[string]string {
			r[key] = c.Request.PostForm.Get(key)
			return r
		}, map[string]string{})
	}
	return lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, key string, _ int) map[string]string {
		r[key] = c.Request.URL.Query().Get(key)
		return r
	}, map[string]string{})
}

func completeWxDaEpayTopUp(c *gin.Context, verifyInfo *service.WxDaEpayVerifyResult) error {
	expectedPaymentMethod := wxDaEpayPaymentMethod(verifyInfo.Type)
	if expectedPaymentMethod == "" || !strings.HasPrefix(expectedPaymentMethod, wxDaEpayPaymentPrefix) {
		return fmt.Errorf("unsupported callback payment type: %s", verifyInfo.Type)
	}

	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)

	topUp := model.GetTopUpByTradeNo(verifyInfo.ServiceTradeNo)
	if topUp == nil {
		return fmt.Errorf("充值订单不存在")
	}
	if topUp.PaymentMethod != expectedPaymentMethod {
		return model.ErrPaymentMethodMismatch
	}
	if topUp.Status == common.TopUpStatusSuccess {
		return nil
	}
	if topUp.Status != common.TopUpStatusPending {
		return fmt.Errorf("充值订单状态错误")
	}

	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = common.GetTimestamp()
	if err := topUp.Update(); err != nil {
		return err
	}

	dAmount := decimal.NewFromInt(topUp.Amount)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
	if quotaToAdd <= 0 {
		return fmt.Errorf("无效的充值额度")
	}
	if err := model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true); err != nil {
		return err
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("wxDa 支付 充值成功 trade_no=%s user_id=%d client_ip=%s quota_to_add=%d money=%.2f", topUp.TradeNo, topUp.UserId, c.ClientIP(), quotaToAdd, topUp.Money))
	model.RecordTopupLog(topUp.UserId, fmt.Sprintf("使用 wxDa 支付充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money), c.ClientIP(), topUp.PaymentMethod, "wxda_epay")
	return nil
}
