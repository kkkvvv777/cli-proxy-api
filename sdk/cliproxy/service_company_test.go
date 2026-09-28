package cliproxy

import (
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestCompanyConfigCannotHotReload(t *testing.T) {
	initial := &config.Config{CompanyGateway: internalconfig.CompanyGatewayConfig{Enabled: true, DataDir: "private-data"}}
	service := &Service{cfg: initial}
	next := initial.CloneForRuntime()
	next.CompanyGateway.Enabled = false
	next.CompanyGateway.DataDir = "other"
	commit := service.commitConfigUpdate(next)
	if commit.cfg == nil || commit.cfg.CompanyGateway != initial.CompanyGateway {
		t.Fatal("company settings changed in service/executor config")
	}
	if next.CompanyGateway.Enabled || next.CompanyGateway.DataDir != "other" {
		t.Fatal("reload mutated caller config")
	}
}
