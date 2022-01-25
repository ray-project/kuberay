package cmdutil

import (
	"fmt"
	"os"
	"reflect"

	"github.com/spf13/viper"
)

var supportedKeys = map[string]bool{"endpoint": true}

func validateKey(key string) {
	_, ok := supportedKeys[key]
	if !ok {
		keys := reflect.ValueOf(supportedKeys).MapKeys()
		fmt.Printf("key %s is not supported, supported keys are: %v", key, keys)
		os.Exit(1)
	}
}

func SetKeyValPair(key string, value string) {
	validateKey(key)
	viper.Set(key, value)
	if err := viper.WriteConfig(); err != nil {
		fmt.Printf("Not able to write to config file %s\n", viper.ConfigFileUsed())
		fmt.Println(err)
		os.Exit(1)
	}
}

func GetVal(key string) string {
	validateKey(key)
	return viper.GetString(key)
}

func Reset() {
	viper.Set("endpoint", fmt.Sprintf("%s:%s", DefaultRpcAddress, DefaultRpcPort))
	if err := viper.WriteConfig(); err != nil {
		fmt.Printf("Not able to write to config file %s\n", viper.ConfigFileUsed())
		fmt.Println(err)
		os.Exit(1)
	}
}
