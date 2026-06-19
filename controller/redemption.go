package controller

import (
	"crypto/subtle"
	"fmt"
	"html"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const redemptionGenerateTokenEnv = "REDEMPTION_GENERATE_TOKEN"
const redemptionGenerateDefaultPlanIDEnv = "REDEMPTION_GENERATE_DEFAULT_PLAN_ID"

type tokenRedemptionGenerateRequest struct {
	Count              int    `json:"count"`
	Name               string `json:"name"`
	SubscriptionPlanId int    `json:"subscription_plan_id"`
	BaseURL            string `json:"base_url"`
	DocsURL            string `json:"docs_url"`
	ExpiredTime        int64  `json:"expired_time"`
}

type tokenRedemptionGenerateResponse struct {
	Keys []string `json:"keys"`
	Text string   `json:"text"`
}

type tokenRedemptionSubscriptionPlan struct {
	Id            int     `json:"id"`
	Title         string  `json:"title"`
	Subtitle      string  `json:"subtitle"`
	PriceAmount   float64 `json:"price_amount"`
	Currency      string  `json:"currency"`
	DurationUnit  string  `json:"duration_unit"`
	DurationValue int     `json:"duration_value"`
	SortOrder     int     `json:"sort_order"`
}

func GetAllRedemptions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.GetAllRedemptions(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func SearchRedemptions(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.SearchRedemptions(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetRedemption(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption, err := model.GetRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    redemption,
	})
	return
}

func AddRedemption(c *gin.Context) {
	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption.Type = model.NormalizeRedemptionType(redemption.Type)
	if utf8.RuneCountInString(redemption.Name) == 0 || utf8.RuneCountInString(redemption.Name) > 20 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionNameLength)
		return
	}
	if redemption.Count <= 0 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountPositive)
		return
	}
	if redemption.Count > 100 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountMax)
		return
	}
	if redemption.Type == model.RedemptionTypeSubscription {
		if redemption.SubscriptionPlanId <= 0 {
			common.ApiErrorMsg(c, "请选择订阅套餐")
			return
		}
		if _, err := model.GetSubscriptionPlanById(redemption.SubscriptionPlanId); err != nil {
			common.ApiErrorMsg(c, "兑换码绑定的订阅套餐不存在")
			return
		}
		redemption.Quota = 0
	} else {
		redemption.SubscriptionPlanId = 0
	}
	if valid, msg := validateExpiredTime(c, redemption.ExpiredTime); !valid {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	var keys []string
	for i := 0; i < redemption.Count; i++ {
		key := common.GetUUID()
		cleanRedemption := model.Redemption{
			UserId:             c.GetInt("id"),
			Name:               redemption.Name,
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			Quota:              redemption.Quota,
			Type:               redemption.Type,
			SubscriptionPlanId: redemption.SubscriptionPlanId,
			ExpiredTime:        redemption.ExpiredTime,
		}
		err = cleanRedemption.Insert()
		if err != nil {
			common.SysError("failed to insert redemption: " + err.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgRedemptionCreateFailed),
				"data":    keys,
			})
			return
		}
		keys = append(keys, key)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    keys,
	})
	return
}

func GenerateRedemptionWithToken(c *gin.Context) {
	if !validateRedemptionGenerateToken(c) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid token",
		})
		return
	}
	req, err := bindRedemptionGenerateRequest(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 100 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountMax)
		return
	}
	if req.SubscriptionPlanId <= 0 {
		req.SubscriptionPlanId = common.GetEnvOrDefault(redemptionGenerateDefaultPlanIDEnv, 0)
	}
	if req.SubscriptionPlanId <= 0 {
		common.ApiErrorMsg(c, "请选择订阅套餐")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.SubscriptionPlanId)
	if err != nil {
		common.ApiErrorMsg(c, "兑换码绑定的订阅套餐不存在")
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "兑换码绑定的订阅套餐已禁用")
		return
	}
	if req.Name == "" {
		req.Name = strings.TrimSpace(plan.Title)
	}
	shareName := strings.TrimSpace(plan.Title)
	if shareName == "" {
		shareName = req.Name
	}
	if utf8.RuneCountInString(req.Name) == 0 || utf8.RuneCountInString(req.Name) > 20 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionNameLength)
		return
	}
	if valid, msg := validateExpiredTime(c, req.ExpiredTime); !valid {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	baseURL := normalizeSiteURL(firstNonEmpty(req.BaseURL, system_setting.ServerAddress))
	docsURL := normalizeSiteURL(firstNonEmpty(req.DocsURL, operation_setting.GetGeneralSetting().DocsLink))
	keys := make([]string, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		key := common.GetUUID()
		redemption := model.Redemption{
			Name:               req.Name,
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			Type:               model.RedemptionTypeSubscription,
			SubscriptionPlanId: req.SubscriptionPlanId,
			ExpiredTime:        req.ExpiredTime,
		}
		if err := redemption.Insert(); err != nil {
			common.SysError("failed to insert token-generated redemption: " + err.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgRedemptionCreateFailed),
				"data":    tokenRedemptionGenerateResponse{Keys: keys, Text: formatRedemptionShareText(baseURL, docsURL, shareName, keys)},
			})
			return
		}
		keys = append(keys, key)
	}
	result := tokenRedemptionGenerateResponse{
		Keys: keys,
		Text: formatRedemptionShareText(baseURL, docsURL, shareName, keys),
	}
	if c.Request != nil && c.Request.Method == http.MethodGet {
		renderRedemptionSharePage(c, result)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}

func GetRedemptionSubscriptionPlansWithToken(c *gin.Context) {
	if !validateRedemptionGenerateToken(c) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid token",
		})
		return
	}
	var plans []model.SubscriptionPlan
	if err := model.DB.Where("enabled = ?", true).Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.NormalizeSubscriptionPlans(plans)
	result := make([]tokenRedemptionSubscriptionPlan, 0, len(plans))
	defaultPlanId := common.GetEnvOrDefault(redemptionGenerateDefaultPlanIDEnv, 0)
	defaultName := ""
	for _, plan := range plans {
		if plan.Id == defaultPlanId {
			defaultName = strings.TrimSpace(plan.Title)
		}
		result = append(result, tokenRedemptionSubscriptionPlan{
			Id:            plan.Id,
			Title:         plan.Title,
			Subtitle:      plan.Subtitle,
			PriceAmount:   plan.PriceAmount,
			Currency:      plan.Currency,
			DurationUnit:  plan.DurationUnit,
			DurationValue: plan.DurationValue,
			SortOrder:     plan.SortOrder,
		})
	}
	if defaultName == "" && len(plans) > 0 {
		defaultName = strings.TrimSpace(plans[0].Title)
	}
	common.ApiSuccess(c, gin.H{
		"plans":             result,
		"default_plan_id":   defaultPlanId,
		"default_name":      defaultName,
		"default_base_url":  normalizeSiteURL(system_setting.ServerAddress),
		"default_docs_url":  normalizeSiteURL(operation_setting.GetGeneralSetting().DocsLink),
		"has_token_support": true,
	})
}

func DeleteRedemption(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateRedemption(c *gin.Context) {
	statusOnly := c.Query("status_only")
	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption.Type = model.NormalizeRedemptionType(redemption.Type)
	cleanRedemption, err := model.GetRedemptionById(redemption.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if statusOnly == "" {
		if redemption.Type == model.RedemptionTypeSubscription {
			if redemption.SubscriptionPlanId <= 0 {
				common.ApiErrorMsg(c, "请选择订阅套餐")
				return
			}
			if _, err := model.GetSubscriptionPlanById(redemption.SubscriptionPlanId); err != nil {
				common.ApiErrorMsg(c, "兑换码绑定的订阅套餐不存在")
				return
			}
			redemption.Quota = 0
		} else {
			redemption.SubscriptionPlanId = 0
		}
		if valid, msg := validateExpiredTime(c, redemption.ExpiredTime); !valid {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
			return
		}
		// If you add more fields, please also update redemption.Update()
		cleanRedemption.Name = redemption.Name
		cleanRedemption.Quota = redemption.Quota
		cleanRedemption.Type = redemption.Type
		cleanRedemption.SubscriptionPlanId = redemption.SubscriptionPlanId
		cleanRedemption.ExpiredTime = redemption.ExpiredTime
	}
	if statusOnly != "" {
		cleanRedemption.Status = redemption.Status
	}
	err = cleanRedemption.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanRedemption,
	})
	return
}

func DeleteInvalidRedemption(c *gin.Context) {
	rows, err := model.DeleteInvalidRedemptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

func validateExpiredTime(c *gin.Context, expired int64) (bool, string) {
	if expired != 0 && expired < common.GetTimestamp() {
		return false, i18n.T(c, i18n.MsgRedemptionExpireTimeInvalid)
	}
	return true, ""
}

func validateRedemptionGenerateToken(c *gin.Context) bool {
	expected := strings.TrimSpace(os.Getenv(redemptionGenerateTokenEnv))
	if expected == "" {
		return false
	}
	provided := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(provided), "bearer ") {
		provided = strings.TrimSpace(provided[7:])
	}
	if provided == "" {
		provided = strings.TrimSpace(c.GetHeader("X-Redemption-Token"))
	}
	if provided == "" {
		provided = strings.TrimSpace(c.Query("token"))
	}
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func formatRedemptionShareText(baseURL string, docsURL string, name string, keys []string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "订阅"
	}
	return fmt.Sprintf("注册地址：%s\n文档地址：%s\n%s兑换码：\n%s", baseURL, docsURL, name, strings.Join(keys, "\n"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeSiteURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func bindRedemptionGenerateRequest(c *gin.Context) (tokenRedemptionGenerateRequest, error) {
	req := tokenRedemptionGenerateRequest{}
	if c.Request != nil && c.Request.Method == http.MethodGet {
		req.Count = parseIntQuery(c, "count", 0)
		req.Name = strings.TrimSpace(c.Query("name"))
		req.SubscriptionPlanId = parseIntQuery(c, "subscription_plan_id", 0)
		req.BaseURL = strings.TrimSpace(c.Query("base_url"))
		req.DocsURL = strings.TrimSpace(c.Query("docs_url"))
		req.ExpiredTime = parseInt64Query(c, "expired_time", 0)
		return req, nil
	}
	if c.Request != nil && c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			return req, err
		}
		return req, nil
	}
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		return req, err
	}
	return req, nil
}

func parseIntQuery(c *gin.Context, key string, defaultValue int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func parseInt64Query(c *gin.Context, key string, defaultValue int64) int64 {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func renderRedemptionSharePage(c *gin.Context, result tokenRedemptionGenerateResponse) {
	text := html.EscapeString(result.Text)
	keysCount := len(result.Keys)
	c.Header("Cache-Control", "no-store, max-age=0")
	page := `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="referrer" content="no-referrer">
  <title>兑换码生成结果</title>
  <style>
    :root { color-scheme: light dark; }
    body { margin: 0; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f6f7fb; color: #111827; }
    main { max-width: 900px; margin: 0 auto; padding: 32px 20px 40px; }
    .panel { background: #fff; border: 1px solid #e5e7eb; border-radius: 10px; padding: 20px; box-shadow: 0 1px 2px rgba(0,0,0,.03); }
    h1 { margin: 0 0 12px; font-size: 22px; line-height: 1.3; }
    p { margin: 0 0 16px; color: #4b5563; }
    textarea { width: 100%; min-height: 180px; box-sizing: border-box; padding: 16px; border: 1px solid #d1d5db; border-radius: 8px; font: 14px/1.6 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; white-space: pre-wrap; resize: vertical; background: #f9fafb; color: #111827; }
    .actions { display: flex; gap: 12px; margin-top: 16px; flex-wrap: wrap; align-items: center; }
    button { border: 0; border-radius: 8px; padding: 10px 16px; cursor: pointer; font-size: 14px; background: #111827; color: #fff; }
    .meta { font-size: 13px; color: #6b7280; }
    .status { margin-top: 10px; font-size: 13px; color: #047857; min-height: 1.2em; }
    code { background: #eef2ff; padding: 2px 6px; border-radius: 4px; }
  </style>
</head>
<body>
  <main>
    <div class="panel">
      <h1>兑换码生成结果</h1>
      <p>已生成 <code>` + strconv.Itoa(keysCount) + `</code> 个兑换码，点击按钮即可复制下方文案。</p>
      <textarea id="shareText" readonly>` + text + `</textarea>
      <div class="actions">
        <button id="copyButton" type="button">复制文案</button>
        <span class="meta">复制后可直接粘贴发送。</span>
      </div>
      <div id="status" class="status"></div>
    </div>
  </main>
  <script>
    const shareText = document.getElementById('shareText');
    const copyButton = document.getElementById('copyButton');
    const status = document.getElementById('status');
    const clearTokenFromUrl = () => {
      try {
        const url = new URL(window.location.href);
        url.searchParams.delete('token');
        window.history.replaceState({}, document.title, url.pathname + url.search + url.hash);
      } catch (_) {}
    };
    const setStatus = (text) => { status.textContent = text; };
    const copyText = async () => {
      try {
        await navigator.clipboard.writeText(shareText.value);
        setStatus('已复制');
      } catch (_) {
        shareText.focus();
        shareText.select();
        const ok = document.execCommand('copy');
        setStatus(ok ? '已复制' : '请手动复制');
      }
    };
    copyButton.addEventListener('click', copyText);
    shareText.addEventListener('focus', () => shareText.select());
    clearTokenFromUrl();
    setStatus('页面已生成，可直接复制。');
  </script>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}
