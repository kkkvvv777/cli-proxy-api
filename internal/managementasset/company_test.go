package managementasset

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestCompanyPanelNeverAutoUpdates(t *testing.T) {
	cfg := &config.Config{CompanyGateway: config.CompanyGatewayConfig{Enabled: true}}
	if _, skip := autoUpdateSkipReason(cfg); !skip {
		t.Fatal("company panel could be overwritten by the upstream bundle")
	}
}
