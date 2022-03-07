package operatorconfig

import (
	"errors"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/fsnotify/fsnotify"
	v1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/spf13/viper"
	// https://github.com/kubernetes-sigs/kubebuilder/issues/1930
	"sigs.k8s.io/yaml"
)

var log = logf.Log.WithName("RayCluster-Controller-Config")

type OperatorConfig struct {
	SlsEndpoint        string `json:"slsEndpoint"`
	SlsAccessKeyID     string `json:"slsAccessKeyID"`
	SlsAccessKeySecret string `json:"slsAccessKeySecret"`
	SlsProject         string `json:"slsProject"`
	SlsLogstore        string `json:"slsLogstore"`
	ArcStudioAddress   string `json:"arcStudioAddress"`
	AntcAddress        string `json:"antcAddress"`
	AntcAccessKey      string `json:"antcAccessKey"`
	AntcSecretKey      string `json:"antcSecretKey"`
	//deprecated
	EventLogPath                          string                     `json:"eventLogPath"`
	PodInfoSchedulingPeriodSecond         string                     `json:"podInfoSchedulingPeriodSecond"`
	PodInfoHTTPRequestIntervalMillisecond string                     `json:"podInfoHTTPRequestIntervalMillisecond"`
	PodDetectPeriodSecond                 string                     `json:"podDetectPeriodSecond"`
	ClusterDetectPeriodSecond             string                     `json:"clusterDetectPeriodSecond"`
	ClusterCreateTimeoutInSecond          string                     `json:"clusterCreateTimeoutInSecond"`
	DisableAutoScaler                     bool                       `json:"disableAutoScaler"`
	PontusAccessKey                       string                     `json:"pontusAccessKey"`
	HpaWhiteList                          map[string]HpaConfig       `json:"hpaWhiteList"`
	HpaBlackList                          map[string]string          `json:"hpaBlackList"`
	DingUrls                              []string                   `json:"dingUrls"`
	RayssUrl                              string                     `json:"rayssUrl"`
	Affinity                              map[string]*v1.Affinity    `json:"affinity"`
	Tolerations                           map[string][]v1.Toleration `json:"tolerations"`
	CougarWhiteList                       []string                   `yaml:"cougarWhiteList"`
	ClusterReconcileWaitSeconds           string                     `yaml:"clusterReconcileWaitSeconds"`

	ClusterLabel      map[string]string `json:"clusterLabel"`
	ClusterAnnotation map[string]string `json:"clusterAnnotation"`
}

var Config = &OperatorConfig{}

var Debug = false

func InitOperatorConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return errors.New("fail to get home dir")
	}
	configFilePath := home + "/conf/operatorconfig.yaml"
	Config, err = Load(configFilePath)
	if err != nil {
		return nil
	}
	viper.SetConfigFile(configFilePath)
	viper.OnConfigChange(func(e fsnotify.Event) {
		tmpConfig, err := Load(e.Name)
		if err != nil {
			log.Error(err, "Failed to load changed config")
		} else {
			Config = tmpConfig
		}
	})
	go viper.WatchConfig()
	return nil
}

func Load(configFile string) (*OperatorConfig, error) {
	content, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	return LoadFrom(content)
}

func LoadFrom(content []byte) (*OperatorConfig, error) {
	var oc OperatorConfig
	if err := yaml.Unmarshal(content, &oc); err != nil {
		return nil, err
	}
	return &oc, nil
}

func DisableAutoScaler() bool {
	return Config.DisableAutoScaler
}

type HpaConfig map[string]int

func GetIntValue(config string, defaultValue int) int {
	if config != "" {
		valueInt, err := strconv.Atoi(config)
		if err == nil {
			return valueInt
		}
	}
	return defaultValue
}
