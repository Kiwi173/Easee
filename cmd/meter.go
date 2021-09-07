package cmd

import (
	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/server"
	"github.com/evcc-io/evcc/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// meterCmd represents the meter command
var meterCmd = &cobra.Command{
	Use:   "meter [name]",
	Short: "Query configured meters",
	Run:   runMeter,
}

func init() {
	rootCmd.AddCommand(meterCmd)
}

func runMeter(cmd *cobra.Command, args []string) {
	util.LogLevel(viper.GetString("log"), viper.GetStringMapString("levels"))
	log.Infof("evcc %s (%s)", server.Version, server.Commit)

	// load config
	conf, err := loadConfigFile(cfgFile)
	if err != nil {
		log.FATAL.Fatal(err)
	}

	// setup environment
	if err := configureEnvironment(conf); err != nil {
		log.FATAL.Fatal(err)
	}

	if err := cp.configureMeters(conf); err != nil {
		log.FATAL.Fatal(err)
	}

	meters := cp.meters
	if len(args) == 1 {
		arg := args[0]
		meters = map[string]api.Meter{arg: cp.Meter(arg)}
	}

	d := dumper{len: len(meters)}
	for name, v := range meters {
		d.DumpWithHeader(name, v)
	}
}
