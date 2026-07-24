package operation_setting

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type PaymentSetting struct {
	AmountOptions         []int           `json:"amount_options"`
	AmountDiscount        map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	ExternalPurchaseLinks map[int]string  `json:"external_purchase_links"`
}

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:         []int{10, 20, 50, 100, 200, 500},
	AmountDiscount:        map[int]float64{},
	ExternalPurchaseLinks: map[int]string{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func GetExternalPurchaseLinks() map[int]string {
	result := make(map[int]string)
	for amount, rawLink := range paymentSetting.ExternalPurchaseLinks {
		link := strings.TrimSpace(rawLink)
		parsed, err := url.Parse(link)
		if amount <= 0 || err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			continue
		}
		result[amount] = parsed.String()
	}
	return result
}
