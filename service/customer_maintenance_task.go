package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/bytedance/gopkg/util/gopool"
)

const customerMaintenanceNotificationInterval = 30 * time.Minute

var (
	customerMaintenanceNotificationOnce    sync.Once
	customerMaintenanceNotificationRunning atomic.Bool
)

func customerMaintenanceModuleEnabled(rawConfig string) bool {
	if strings.TrimSpace(rawConfig) == "" {
		return false
	}
	modules := map[string]interface{}{}
	if err := common.UnmarshalJsonStr(rawConfig, &modules); err != nil {
		return false
	}
	switch value := modules["customerMaintenance"].(type) {
	case bool:
		return value
	case map[string]interface{}:
		enabled, ok := value["enabled"].(bool)
		return ok && enabled
	default:
		return false
	}
}

func customerMaintenanceNotificationsEnabled() bool {
	rawConfig := strings.TrimSpace(os.Getenv("NEWAPI_HEADER_NAV_MODULES"))
	if rawConfig == "" {
		common.OptionMapRWMutex.RLock()
		rawConfig = common.OptionMap["HeaderNavModules"]
		common.OptionMapRWMutex.RUnlock()
	}
	return customerMaintenanceModuleEnabled(rawConfig)
}

// StartCustomerMaintenanceNotificationTask keeps subscription-expiry notices
// current only on sites that explicitly enable the customerMaintenance module.
// The outbox remains local and no external robot endpoint is called here.
func StartCustomerMaintenanceNotificationTask() {
	customerMaintenanceNotificationOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(customerMaintenanceNotificationInterval)
			defer ticker.Stop()

			runCustomerMaintenanceNotificationOnce()
			for range ticker.C {
				runCustomerMaintenanceNotificationOnce()
			}
		})
	})
}

func runCustomerMaintenanceNotificationOnce() {
	if !customerMaintenanceNotificationsEnabled() {
		return
	}
	if !customerMaintenanceNotificationRunning.CompareAndSwap(false, true) {
		return
	}
	defer customerMaintenanceNotificationRunning.Store(false)

	result, err := BackfillExpiredMonthlySubscriptionNotifications(2, time.Now())
	if err != nil {
		logger.LogWarn(context.Background(), "customer maintenance notification scan failed: "+err.Error())
		return
	}
	if result.Created > 0 || result.SupersededRenewals > 0 {
		logger.LogInfo(
			context.Background(),
			fmt.Sprintf(
				"customer maintenance notification scan reconciled records: created=%d, existing=%d, superseded_renewals=%d",
				result.Created,
				result.Existing,
				result.SupersededRenewals,
			),
		)
	}
}
