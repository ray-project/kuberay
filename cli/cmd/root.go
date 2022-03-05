package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/ray-project/kuberay/cli/pkg/cmd/cluster"
	"github.com/ray-project/kuberay/cli/pkg/cmd/config"
	"github.com/ray-project/kuberay/cli/pkg/cmd/info"
	"github.com/ray-project/kuberay/cli/pkg/cmd/template"
	"github.com/ray-project/kuberay/cli/pkg/cmd/version"
	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/spf13/cobra"

	"github.com/fatih/color"
	"github.com/kris-nova/logger"
	lol "github.com/kris-nova/lolgopher"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "kuberay",
	Short: "kuberay offers life cycle management of ray clusters",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	loggerLevel := rootCmd.PersistentFlags().IntP("log-level", "l", 3, "set log level, use 0 to silence, 4 for debugging and 5 for debugging with AWS debug logging")
	colorValue := rootCmd.PersistentFlags().StringP("color", "C", "true", "toggle colorized logs (valid options: true, false, fabulous)")
	cobra.OnInitialize(initConfig, func() {
		initLogger(*loggerLevel, *colorValue)
	})

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kuberay.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.PersistentFlags().BoolP("help", "h", false, "help for this command")
	rootCmd.AddCommand(info.NewCmdInfo())
	rootCmd.AddCommand(version.NewCmdVersion())
	rootCmd.AddCommand(cluster.NewCmdCluster())
	rootCmd.AddCommand(template.NewCmdTemplate())
	rootCmd.AddCommand(config.NewCmdConfig())
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".cli" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".kuberay")

		viper.SetDefault("endpoint", fmt.Sprintf("%s:%s", cmdutil.DefaultRpcAddress, cmdutil.DefaultRpcPort))
		// Do not write to file system if it already exists
		if err := viper.SafeWriteConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileAlreadyExistsError); !ok {
				klog.Fatal(err)
			}
		}
	}

	viper.AutomaticEnv() // read in environment variables that match

	if err := viper.ReadInConfig(); err != nil {
		klog.Fatal(err)
	}
}

func initLogger(level int, colorValue string) {
	logger.Layout = "2021-01-02 15:04:05"

	var bitwiseLevel int
	switch level {
	case 4:
		bitwiseLevel = logger.LogDeprecated | logger.LogAlways | logger.LogSuccess | logger.LogCritical | logger.LogWarning | logger.LogInfo | logger.LogDebug
	case 3:
		bitwiseLevel = logger.LogDeprecated | logger.LogAlways | logger.LogSuccess | logger.LogCritical | logger.LogWarning | logger.LogInfo
	case 2:
		bitwiseLevel = logger.LogDeprecated | logger.LogAlways | logger.LogSuccess | logger.LogCritical | logger.LogWarning
	case 1:
		bitwiseLevel = logger.LogDeprecated | logger.LogAlways | logger.LogSuccess | logger.LogCritical
	case 0:
		bitwiseLevel = logger.LogDeprecated | logger.LogAlways | logger.LogSuccess
	default:
		bitwiseLevel = logger.LogDeprecated | logger.LogEverything
	}
	logger.BitwiseLevel = bitwiseLevel

	switch colorValue {
	case "fabulous":
		logger.Writer = lol.NewLolWriter()
	case "true":
		logger.Writer = color.Output
	}

	logger.Line = func(prefix, format string, a ...interface{}) string {
		if !strings.Contains(format, "\n") {
			format = fmt.Sprintf("%s%s", format, "\n")
		}
		now := time.Now()
		fNow := now.Format(logger.Layout)
		var colorize func(format string, a ...interface{}) string
		var icon string
		switch prefix {
		case logger.PreAlways:
			icon = "✿"
			colorize = color.GreenString
		case logger.PreCritical:
			icon = "✖"
			colorize = color.RedString
		case logger.PreInfo:
			icon = "ℹ"
			colorize = color.CyanString
		case logger.PreDebug:
			icon = "▶"
			colorize = color.GreenString
		case logger.PreSuccess:
			icon = "✔"
			colorize = color.CyanString
		case logger.PreWarning:
			icon = "!"
			colorize = color.GreenString
		default:
			icon = "ℹ"
			colorize = color.CyanString
		}

		out := fmt.Sprintf(format, a...)
		out = fmt.Sprintf("%s [%s]  %s", fNow, icon, out)
		if colorValue == "true" {
			out = colorize(out)
		}

		return out
	}
}
