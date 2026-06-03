package service

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

const (
	WxDaEpaySignTypeMD5 = "MD5"
	WxDaEpaySignTypeRSA = "RSA"

	WxDaEpayTradeSuccess = "TRADE_SUCCESS"
)

type WxDaEpayPurchaseArgs struct {
	Type           string
	ServiceTradeNo string
	Name           string
	Money          string
	NotifyURL      string
	ReturnURL      string
}

type WxDaEpayVerifyResult struct {
	Type           string
	TradeNo        string
	ServiceTradeNo string
	Name           string
	Money          string
	TradeStatus    string
	VerifyStatus   bool
}

func NormalizeWxDaEpaySignType(signType string) string {
	signType = strings.ToUpper(strings.TrimSpace(signType))
	if signType == WxDaEpaySignTypeRSA {
		return WxDaEpaySignTypeRSA
	}
	return WxDaEpaySignTypeMD5
}

func GetWxDaEpaySubmitPath() string {
	submitPath := strings.TrimSpace(setting.WxDaEpaySubmitPath)
	if submitPath != "" {
		if strings.HasPrefix(submitPath, "/") {
			return submitPath
		}
		return "/" + submitPath
	}
	if NormalizeWxDaEpaySignType(setting.WxDaEpaySignType) == WxDaEpaySignTypeRSA {
		return "/api/pay/submit"
	}
	return "/submit.php"
}

func BuildWxDaEpayPurchase(args *WxDaEpayPurchaseArgs) (string, map[string]string, error) {
	baseURL := strings.TrimSpace(setting.WxDaEpayAddress)
	if baseURL == "" {
		return "", nil, errors.New("wxDa epay address is empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", nil, err
	}
	u.Path = path.Join(u.Path, GetWxDaEpaySubmitPath())

	params := map[string]string{
		"pid":          strings.TrimSpace(setting.WxDaEpayPid),
		"type":         args.Type,
		"out_trade_no": args.ServiceTradeNo,
		"notify_url":   args.NotifyURL,
		"return_url":   args.ReturnURL,
		"name":         args.Name,
		"money":        args.Money,
	}

	signType := NormalizeWxDaEpaySignType(setting.WxDaEpaySignType)
	if signType == WxDaEpaySignTypeRSA {
		params["timestamp"] = fmt.Sprintf("%d", time.Now().Unix())
		sign, err := wxDaEpayRSASign(params, setting.WxDaEpayMerchantPrivateKey)
		if err != nil {
			return "", nil, err
		}
		params["sign"] = sign
		params["sign_type"] = WxDaEpaySignTypeRSA
	} else {
		params["device"] = "pc"
		params["sign"] = wxDaEpayMD5Sign(params, setting.WxDaEpayMD5Key)
		params["sign_type"] = WxDaEpaySignTypeMD5
	}

	return u.String(), params, nil
}

func VerifyWxDaEpay(params map[string]string) (*WxDaEpayVerifyResult, error) {
	if len(params) == 0 {
		return nil, errors.New("empty wxDa epay params")
	}
	result := &WxDaEpayVerifyResult{
		Type:           params["type"],
		TradeNo:        params["trade_no"],
		ServiceTradeNo: params["out_trade_no"],
		Name:           params["name"],
		Money:          params["money"],
		TradeStatus:    params["trade_status"],
	}

	sign := params["sign"]
	if sign == "" {
		return result, nil
	}
	signType := NormalizeWxDaEpaySignType(params["sign_type"])
	if signType == WxDaEpaySignTypeRSA {
		if !wxDaEpayTimestampValid(params["timestamp"]) {
			return result, nil
		}
		ok, err := wxDaEpayRSAVerify(params, sign, setting.WxDaEpayPlatformPublicKey)
		if err != nil {
			return nil, err
		}
		result.VerifyStatus = ok
		return result, nil
	}

	expected := wxDaEpayMD5Sign(params, setting.WxDaEpayMD5Key)
	result.VerifyStatus = subtle.ConstantTimeCompare([]byte(strings.ToLower(sign)), []byte(expected)) == 1
	return result, nil
}

func wxDaEpayTimestampValid(timestamp string) bool {
	if strings.TrimSpace(timestamp) == "" {
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	return ts >= now-300 && ts <= now+300
}

func wxDaEpayMD5Sign(params map[string]string, key string) string {
	content := wxDaEpaySignContent(params)
	digest := md5.Sum([]byte(content + strings.TrimSpace(key)))
	return fmt.Sprintf("%x", digest)
}

func wxDaEpayRSASign(params map[string]string, privateKey string) (string, error) {
	key, err := parseWxDaEpayPrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(wxDaEpaySignContent(params)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func wxDaEpayRSAVerify(params map[string]string, sign string, publicKey string) (bool, error) {
	key, err := parseWxDaEpayPublicKey(publicKey)
	if err != nil {
		return false, err
	}
	signature, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256([]byte(wxDaEpaySignContent(params)))
	err = rsa.VerifyPKCS1v15(key, crypto.SHA256, hash[:], signature)
	return err == nil, nil
}

func wxDaEpaySignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "sign" || key == "sign_type" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, "&")
}

func parseWxDaEpayPrivateKey(privateKey string) (*rsa.PrivateKey, error) {
	block, err := parseWxDaEpayPEMBlock(privateKey, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("private key is not RSA")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func parseWxDaEpayPublicKey(publicKey string) (*rsa.PublicKey, error) {
	block, err := parseWxDaEpayPEMBlock(publicKey, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not RSA")
	}
	return rsaKey, nil
}

func parseWxDaEpayPEMBlock(key string, blockType string) (*pem.Block, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("empty key")
	}
	if !strings.Contains(key, "-----BEGIN") {
		key = "-----BEGIN " + blockType + "-----\n" + wordWrapWxDaEpayKey(key) + "\n-----END " + blockType + "-----"
	}
	block, _ := pem.Decode([]byte(key))
	if block == nil {
		return nil, errors.New("invalid PEM key")
	}
	return block, nil
}

func wordWrapWxDaEpayKey(key string) string {
	key = strings.ReplaceAll(key, "\n", "")
	key = strings.ReplaceAll(key, "\r", "")
	key = strings.ReplaceAll(key, " ", "")

	var b strings.Builder
	for len(key) > 64 {
		b.WriteString(key[:64])
		b.WriteByte('\n')
		key = key[64:]
	}
	b.WriteString(key)
	return b.String()
}
