package operatorconfig

import (
	"testing"
)

func TestLoadFrom(t *testing.T) {
	content := "slsEndpoint: cn-test-ant-eu95.log.aliyuncs.co\n" +
		"slsAccessKeyID: ak\n" +
		"hpaWhiteList: \n" +
		"  raycluster1:\n" +
		"    \"cpu\": 20\n" +
		"    \"timeWindow\": 5\n" +
		"  raycluster2: \n" +
		"dingUrls: \n" +
		"- xxx\n" +
		"- yyy\n" +
		"aff:\n" +
		"  NodeAffinity:\n" +
		"    RequiredDuringSchedulingIgnoredDuringExecution:\n" +
		"      NodeSelectorTerms:\n" +
		"        - MatchExpressions:\n" +
		"          - Key: \"sigma.ali/is-over-quota\"\n" +
		"            Operator: In\n" +
		"            Values:\n" +
		"            - \"true\"\n" +
		"affinity:\n" +
		"  overquotatest:\n" +
		"    nodeAffinity:\n" +
		"      requiredDuringSchedulingIgnoredDuringExecution:\n" +
		"        nodeSelectorTerms:\n" +
		"          - matchExpressions:\n" +
		"            - key: mandatory.k8s.alipay.com/app-env-test\n" +
		"              operator: In\n" +
		"              values:\n" +
		"              - gray-test\n" +
		"tolerations:\n" +
		"  overquotatest:\n" +
		"    - effect: NoSchedule\n" +
		"      key: sigma.ali/is-over-quota-test\n" +
		"      operator: Equal\n" +
		"      value: \"true\"\n"
	t.Log(content)
	config, err := LoadFrom([]byte(content))
	if err != nil {
		t.Fatal(err)
	} else {
		if config.SlsAccessKeyID != "ak" {
			t.Fatalf("error load %v", config)
		}

		hpa1 := config.HpaWhiteList["raycluster1"]
		if !(hpa1 != nil && hpa1["cpu"] == 20) {
			t.Fatalf("error load %v", config)
		}

		hpa2, contains := config.HpaWhiteList["raycluster2"]
		if !(contains && hpa2 == nil) {
			t.Fatalf("error load %v", config)
		}

		dingUrls := config.DingUrls
		if len(dingUrls) != 2 {
			t.Fatalf("error load %v,dingUrls error", config)
		}
		if dingUrls[0] != "xxx" || dingUrls[1] != "yyy" {
			t.Fatalf("error load %v,dingUrls error", config)
		}
	}
}

func TestAaa(t *testing.T) {
	map1 := make(map[string]interface{})
	map1["a"] = true
	t.Log(map1["a"] == true)
}
