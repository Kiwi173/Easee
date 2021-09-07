package cmd

import (
	"crypto/x509/pkix"
	"os"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	certhelper "github.com/evcc-io/eebus/cert"
	"github.com/evcc-io/eebus/communication"
	"github.com/evcc-io/evcc/server"
	"github.com/evcc-io/evcc/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// teslaCmd represents the vehicle command
var eebusCmd = &cobra.Command{
	Use:   "eebus-cert",
	Short: "Generate EEBUS certificate for using EEBUS compatible chargers",
	Run:   runEEBUSCert,
}

func init() {
	rootCmd.AddCommand(eebusCmd)
}

const tmpl = `
Add the following to the evcc config file:

eebus:
  certificate:
    public: |
{{ .public | indent 6 }}
    private: |
{{ .private | indent 6 }}
`

func generateEEBUSCert() {
	details := communication.ManufacturerDetails{
		DeviceName:    "EVCC",
		DeviceCode:    "EVCC_HEMS_01",
		DeviceAddress: "EVCC_HEMS",
		BrandName:     "EVCC",
	}

	subject := pkix.Name{
		CommonName:   details.DeviceCode,
		Country:      []string{"DE"},
		Organization: []string{details.BrandName},
	}

	cert, err := certhelper.CreateCertificate(true, subject)
	if err != nil {
		log.FATAL.Fatal("could not create certificate")
	}

	pubKey, privKey, err := certhelper.GetX509KeyPair(cert)
	if err != nil {
		log.FATAL.Fatal("could not process generated certificate")
	}

	t := template.Must(template.New("out").Funcs(template.FuncMap(sprig.FuncMap())).Parse(tmpl))
	if err := t.Execute(os.Stdout, map[string]interface{}{
		"public":  pubKey,
		"private": privKey,
	}); err != nil {
		log.FATAL.Fatal("rendering failed", err)
	}
}

func runEEBUSCert(cmd *cobra.Command, args []string) {
	util.LogLevel(viper.GetString("log"), viper.GetStringMapString("levels"))
	log.Infof("evcc %s (%s)", server.Version, server.Commit)

	generateEEBUSCert()
}
