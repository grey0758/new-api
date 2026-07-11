package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const installCommandConfigOptionKey = "InstallCommandConfig"

type installCommandConfig struct {
	Models                  []string `json:"models,omitempty"`
	DefaultModel            string   `json:"default_model,omitempty"`
	ReasoningEfforts        []string `json:"reasoning_efforts,omitempty"`
	DefaultReasoningEffort  string   `json:"default_reasoning_effort,omitempty"`
	ApprovalPolicy          string   `json:"approval_policy"`
	SandboxMode             string   `json:"sandbox_mode"`
	SupportsWebsockets      bool     `json:"supports_websockets"`
	WorkspaceName           string   `json:"workspace_name"`
	WindowsProjectPathStyle string   `json:"windows_project_path_style"`
	LaunchBypassFlag        bool     `json:"launch_bypass_flag"`
}

func defaultInstallCommandConfig() installCommandConfig {
	return installCommandConfig{
		Models:                  []string{"gpt-5.5", "gpt-5.6-sol"},
		DefaultModel:            "gpt-5.6-sol",
		ReasoningEfforts:        []string{"xhigh", "max"},
		DefaultReasoningEffort:  "xhigh",
		ApprovalPolicy:          "never",
		SandboxMode:             "danger-full-access",
		SupportsWebsockets:      false,
		WorkspaceName:           "opencodex-workspace",
		WindowsProjectPathStyle: "forward-slash",
		LaunchBypassFlag:        false,
	}
}

func publicInstallCommandConfig(raw string) installCommandConfig {
	config := defaultInstallCommandConfig()
	if strings.TrimSpace(raw) != "" {
		var parsed installCommandConfig
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			if len(parsed.Models) > 0 {
				config.Models = sanitizeInstallModels(parsed.Models, config.Models)
			}
			if isSafeInstallModel(parsed.DefaultModel) {
				config.DefaultModel = parsed.DefaultModel
			}
			if len(parsed.ReasoningEfforts) > 0 {
				config.ReasoningEfforts = sanitizeInstallReasoningEfforts(parsed.ReasoningEfforts, config.ReasoningEfforts)
			}
			if isSafeInstallReasoningEffort(parsed.DefaultReasoningEffort) {
				config.DefaultReasoningEffort = parsed.DefaultReasoningEffort
			}
			if parsed.ApprovalPolicy != "" {
				config.ApprovalPolicy = sanitizeApprovalPolicy(parsed.ApprovalPolicy, config.ApprovalPolicy)
			}
			if parsed.SandboxMode != "" {
				config.SandboxMode = sanitizeSandboxMode(parsed.SandboxMode, config.SandboxMode)
			}
			config.SupportsWebsockets = parsed.SupportsWebsockets
			if parsed.WorkspaceName != "" {
				config.WorkspaceName = sanitizeWorkspaceName(parsed.WorkspaceName, config.WorkspaceName)
			}
			if parsed.WindowsProjectPathStyle != "" {
				config.WindowsProjectPathStyle = sanitizeWindowsProjectPathStyle(parsed.WindowsProjectPathStyle, config.WindowsProjectPathStyle)
			}
			config.LaunchBypassFlag = parsed.LaunchBypassFlag
		}
	}
	if !containsString(config.Models, config.DefaultModel) {
		config.DefaultModel = config.Models[0]
	}
	if !containsString(config.ReasoningEfforts, config.DefaultReasoningEffort) {
		config.DefaultReasoningEffort = config.ReasoningEfforts[0]
	}
	return config
}

func sanitizeApprovalPolicy(value string, fallback string) string {
	switch strings.TrimSpace(value) {
	case "never", "on-request", "on-failure", "untrusted":
		return strings.TrimSpace(value)
	default:
		return fallback
	}
}

func sanitizeSandboxMode(value string, fallback string) string {
	switch strings.TrimSpace(value) {
	case "danger-full-access", "workspace-write", "read-only":
		return strings.TrimSpace(value)
	default:
		return fallback
	}
}

func sanitizeWindowsProjectPathStyle(value string, fallback string) string {
	switch strings.TrimSpace(value) {
	case "forward-slash", "escaped-backslash":
		return strings.TrimSpace(value)
	default:
		return fallback
	}
}

var installWorkspaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func sanitizeWorkspaceName(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if installWorkspaceNamePattern.MatchString(value) {
		return value
	}
	return fallback
}

var installModelPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,80}$`)

func isSafeInstallModel(value string) bool {
	return installModelPattern.MatchString(strings.TrimSpace(value))
}

func sanitizeInstallModels(values []string, fallback []string) []string {
	models := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !isSafeInstallModel(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		models = append(models, value)
	}
	if len(models) == 0 {
		return fallback
	}
	return models
}

func isSafeInstallReasoningEffort(value string) bool {
	switch strings.TrimSpace(value) {
	case "xhigh", "max":
		return true
	default:
		return false
	}
}

func sanitizeInstallReasoningEfforts(values []string, fallback []string) []string {
	efforts := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !isSafeInstallReasoningEffort(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		efforts = append(efforts, value)
	}
	if len(efforts) == 0 {
		return fallback
	}
	return efforts
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalizeInstallCommandConfigValue(raw string) (string, error) {
	var parsed installCommandConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", err
	}
	normalized := publicInstallCommandConfig(raw)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func TestStatus(c *gin.Context) {
	err := model.PingDB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "数据库连接失败",
		})
		return
	}
	// 获取HTTP统计信息
	httpStats := middleware.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "Server is running",
		"http_stats": httpStats,
	})
	return
}

func GetStatus(c *gin.Context) {

	cs := console_setting.GetConsoleSetting()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	passkeySetting := system_setting.GetPasskeySettings()
	legalSetting := system_setting.GetLegalSettings()
	baseURL := strings.TrimRight(system_setting.ServerAddress, "/")
	installLink := strings.TrimSpace(operation_setting.GetGeneralSetting().InstallLink)
	if envInstallLink := strings.TrimSpace(os.Getenv("NEWAPI_INSTALL_LINK")); envInstallLink != "" {
		installLink = envInstallLink
	}
	if installLink == "" && baseURL != "" {
		installLink = baseURL + "/install/"
	}
	headerNavModules := runtimeHeaderNavModules(common.OptionMap["HeaderNavModules"], installLink)
	installCommandConfig := publicInstallCommandConfig(common.OptionMap[installCommandConfigOptionKey])

	data := gin.H{
		"version":                     common.Version,
		"start_time":                  common.StartTime,
		"email_verification":          common.EmailVerificationEnabled,
		"github_oauth":                common.GitHubOAuthEnabled,
		"github_client_id":            common.GitHubClientId,
		"discord_oauth":               system_setting.GetDiscordSettings().Enabled,
		"discord_client_id":           system_setting.GetDiscordSettings().ClientId,
		"linuxdo_oauth":               common.LinuxDOOAuthEnabled,
		"linuxdo_client_id":           common.LinuxDOClientId,
		"linuxdo_minimum_trust_level": common.LinuxDOMinimumTrustLevel,
		"telegram_oauth":              common.TelegramOAuthEnabled,
		"telegram_bot_name":           common.TelegramBotName,
		"system_name":                 common.SystemName,
		"logo":                        common.Logo,
		"footer_html":                 common.Footer,
		"wechat_qrcode":               common.WeChatAccountQRCodeImageURL,
		"wechat_login":                common.WeChatAuthEnabled,
		"server_address":              system_setting.ServerAddress,
		"turnstile_check":             common.TurnstileCheckEnabled,
		"turnstile_site_key":          common.TurnstileSiteKey,
		"top_up_link":                 common.TopUpLink,
		"docs_link":                   operation_setting.GetGeneralSetting().DocsLink,
		"install_link":                installLink,
		"install_command_config":      installCommandConfig,
		"quota_per_unit":              common.QuotaPerUnit,
		// 兼容旧前端：保留 display_in_currency，同时提供新的 quota_display_type
		"display_in_currency":           operation_setting.IsCurrencyDisplay(),
		"quota_display_type":            operation_setting.GetQuotaDisplayType(),
		"custom_currency_symbol":        operation_setting.GetGeneralSetting().CustomCurrencySymbol,
		"custom_currency_exchange_rate": operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate,
		"enable_batch_update":           common.BatchUpdateEnabled,
		"enable_drawing":                common.DrawingEnabled,
		"enable_task":                   common.TaskEnabled,
		"enable_data_export":            common.DataExportEnabled,
		"data_export_default_time":      common.DataExportDefaultTime,
		"default_collapse_sidebar":      common.DefaultCollapseSidebar,
		"mj_notify_enabled":             setting.MjNotifyEnabled,
		"chats":                         setting.Chats,
		"demo_site_enabled":             operation_setting.DemoSiteEnabled,
		"self_use_mode_enabled":         operation_setting.SelfUseModeEnabled,
		"default_use_auto_group":        setting.DefaultUseAutoGroup,

		"usd_exchange_rate": operation_setting.USDExchangeRate,
		"price":             operation_setting.Price,
		"stripe_unit_price": setting.StripeUnitPrice,

		// 面板启用开关
		"api_info_enabled":      cs.ApiInfoEnabled,
		"uptime_kuma_enabled":   cs.UptimeKumaEnabled,
		"announcements_enabled": cs.AnnouncementsEnabled,
		"faq_enabled":           cs.FAQEnabled,

		// 模块管理配置
		"HeaderNavModules":    headerNavModules,
		"SidebarModulesAdmin": common.OptionMap["SidebarModulesAdmin"],

		"oidc_enabled":                system_setting.GetOIDCSettings().Enabled,
		"oidc_client_id":              system_setting.GetOIDCSettings().ClientId,
		"oidc_authorization_endpoint": system_setting.GetOIDCSettings().AuthorizationEndpoint,
		"passkey_login":               passkeySetting.Enabled,
		"passkey_display_name":        passkeySetting.RPDisplayName,
		"passkey_rp_id":               passkeySetting.RPID,
		"passkey_origins":             passkeySetting.Origins,
		"passkey_allow_insecure":      passkeySetting.AllowInsecureOrigin,
		"passkey_user_verification":   passkeySetting.UserVerification,
		"passkey_attachment":          passkeySetting.AttachmentPreference,
		"setup":                       constant.Setup,
		"user_agreement_enabled":      legalSetting.UserAgreement != "",
		"privacy_policy_enabled":      legalSetting.PrivacyPolicy != "",
		"checkin_enabled":             operation_setting.GetCheckinSetting().Enabled,
	}

	// 根据启用状态注入可选内容
	if cs.ApiInfoEnabled {
		data["api_info"] = console_setting.GetApiInfo()
	}
	if cs.AnnouncementsEnabled {
		data["announcements"] = console_setting.GetAnnouncements()
	}
	if cs.FAQEnabled {
		data["faq"] = console_setting.GetFAQ()
	}

	// Add enabled custom OAuth providers
	customProviders := oauth.GetEnabledCustomProviders()
	if len(customProviders) > 0 {
		type CustomOAuthInfo struct {
			Id                    int    `json:"id"`
			Name                  string `json:"name"`
			Slug                  string `json:"slug"`
			Icon                  string `json:"icon"`
			ClientId              string `json:"client_id"`
			AuthorizationEndpoint string `json:"authorization_endpoint"`
			Scopes                string `json:"scopes"`
		}
		providersInfo := make([]CustomOAuthInfo, 0, len(customProviders))
		for _, p := range customProviders {
			config := p.GetConfig()
			providersInfo = append(providersInfo, CustomOAuthInfo{
				Id:                    config.Id,
				Name:                  config.Name,
				Slug:                  config.Slug,
				Icon:                  config.Icon,
				ClientId:              config.ClientId,
				AuthorizationEndpoint: config.AuthorizationEndpoint,
				Scopes:                config.Scopes,
			})
		}
		data["custom_oauth_providers"] = providersInfo
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
	return
}

func runtimeHeaderNavModules(storedValue string, installLink string) string {
	override := strings.TrimSpace(os.Getenv("NEWAPI_HEADER_NAV_MODULES"))
	if override != "" {
		storedValue = override
	}

	installEnabledValue := strings.TrimSpace(os.Getenv("NEWAPI_INSTALL_ENABLED"))
	envInstallLink := strings.TrimSpace(os.Getenv("NEWAPI_INSTALL_LINK"))
	if installEnabledValue == "" && envInstallLink == "" {
		return storedValue
	}

	modules := map[string]interface{}{}
	if strings.TrimSpace(storedValue) != "" {
		_ = json.Unmarshal([]byte(storedValue), &modules)
	}

	installConfig := map[string]interface{}{}
	switch current := modules["install"].(type) {
	case map[string]interface{}:
		for key, value := range current {
			installConfig[key] = value
		}
	case bool:
		installConfig["enabled"] = current
	}

	if installEnabledValue != "" {
		installConfig["enabled"] = parseEnvBool(installEnabledValue)
	}
	if envInstallLink != "" {
		installConfig["link"] = envInstallLink
	} else if installLink != "" {
		if _, ok := installConfig["link"]; !ok {
			installConfig["link"] = installLink
		}
	}
	modules["install"] = installConfig

	encoded, err := json.Marshal(modules)
	if err != nil {
		return storedValue
	}
	return string(encoded)
}

func parseEnvBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "enabled":
		return true
	default:
		return false
	}
}

func GetNotice(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Notice"],
	})
	return
}

func GetAbout(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["About"],
	})
	return
}

func GetUserAgreement(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetLegalSettings().UserAgreement,
	})
	return
}

func GetPrivacyPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetLegalSettings().PrivacyPolicy,
	})
	return
}

func GetMidjourney(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Midjourney"],
	})
	return
}

func GetHomePageContent(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["HomePageContent"],
	})
	return
}

func SendEmailVerification(c *gin.Context) {
	email := c.Query("email")
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的邮箱地址",
		})
		return
	}
	localPart := parts[0]
	domainPart := parts[1]
	if common.EmailDomainRestrictionEnabled {
		allowed := false
		for _, domain := range common.EmailDomainWhitelist {
			if domainPart == domain {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "The administrator has enabled the email domain name whitelist, and your email address is not allowed due to special symbols or it's not in the whitelist.",
			})
			return
		}
	}
	if common.EmailAliasRestrictionEnabled {
		containsSpecialSymbols := strings.Contains(localPart, "+") || strings.Contains(localPart, ".")
		if containsSpecialSymbols {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员已启用邮箱地址别名限制，您的邮箱地址由于包含特殊符号而被拒绝。",
			})
			return
		}
	}

	if model.IsEmailAlreadyTaken(email) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "邮箱地址已被占用",
		})
		return
	}
	code := common.GenerateVerificationCode(6)
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	subject := fmt.Sprintf("%s邮箱验证邮件", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s邮箱验证。</p>"+
		"<p>您的验证码为: <strong>%s</strong></p>"+
		"<p>验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, code, common.VerificationValidMinutes)
	err := common.SendEmail(subject, email, content)
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

func SendPasswordResetEmail(c *gin.Context) {
	email := c.Query("email")
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	if model.IsEmailAlreadyTaken(email) {
		code := common.GenerateVerificationCode(0)
		common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
		link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", system_setting.ServerAddress, email, code)
		subject := fmt.Sprintf("%s密码重置", common.SystemName)
		content := fmt.Sprintf("<p>您好，你正在进行%s密码重置。</p>"+
			"<p>点击 <a href='%s'>此处</a> 进行密码重置。</p>"+
			"<p>如果链接无法点击，请尝试点击下面的链接或将其复制到浏览器中打开：<br> %s </p>"+
			"<p>重置链接 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, link, link, common.VerificationValidMinutes)
		err := common.SendEmail(subject, email, content)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("failed to send password reset email to %s: %s", email, err.Error()))
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

func ResetPassword(c *gin.Context) {
	var req PasswordResetRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)
	if req.Email == "" || req.Token == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	if !common.VerifyCodeWithKey(req.Email, req.Token, common.PasswordResetPurpose) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "重置链接非法或已过期",
		})
		return
	}
	password := common.GenerateVerificationCode(12)
	err = model.ResetUserPasswordByEmail(req.Email, password)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.DeleteKey(req.Email, common.PasswordResetPurpose)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    password,
	})
	return
}
