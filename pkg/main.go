package main

import (
	"os"

	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/plugin"
)

func main() {
	if err := datasource.Manage(
		"pumba3000lvl-profitmonitor-datasource",
		plugin.NewDatasource,
		datasource.ManageOpts{},
	); err != nil {
		log.DefaultLogger.Error(err.Error())
		os.Exit(1)
	}
}
