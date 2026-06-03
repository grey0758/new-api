package setting

var (
	WxDaEpayEnabled            bool
	WxDaEpayAddress            string
	WxDaEpayPid                string
	WxDaEpaySignType           string = "MD5"
	WxDaEpayMD5Key             string
	WxDaEpayPlatformPublicKey  string
	WxDaEpayMerchantPrivateKey string
	WxDaEpaySubmitPath         string
	WxDaEpayAlipayEnabled      bool = true
	WxDaEpayWxpayEnabled       bool = true
)
