package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// InvitationSetting is scoped to a configured site, never to an HTTP Host header.
type InvitationSetting struct {
	Enabled               bool   `json:"enabled"`
	SiteOrigin            string `json:"site_origin"`
	BalanceRateBPS        int    `json:"balance_rate_bps"`
	SubscriptionRateBPS   int    `json:"subscription_rate_bps"`
	EnabledAt             int64  `json:"enabled_at"`
	MinRedemptionIssuedAt int64  `json:"min_redemption_issued_at"`
}

var invitationSetting = InvitationSetting{BalanceRateBPS: 1000}

func init() { config.GlobalConfig.Register("invitation_setting", &invitationSetting) }

func GetInvitationSetting() *InvitationSetting { return &invitationSetting }
