package utils

import (
	"strings"
	"testing"
)

func TestSlice(t *testing.T) {
	strs := []string{"1", "2", "3", "4", "5"}
	t.Logf("laster %s", strs[len(strs)-1])
	t.Logf("not laster all %s", strings.Join(strs[:len(strs)-1], ""))
	clusterNameId := "a_s_sdf_sdfsdfsdfisfdf1_2safd-0sdf-sdf-000"
	strs = strings.Split(clusterNameId, "_")
	t.Logf("laster %s", strs[len(strs)-1])
	t.Logf("not laster all %s", strings.Join(strs[:len(strs)-1], "_"))

	clusterNameId = "a-s-sdf-sdfsdfsdfisfdf1A_B2safd-0sdf-sdf-000"
	strs = strings.Split(clusterNameId, "_")
	t.Logf("laster %s", strs[len(strs)-1])
	t.Logf("not laster all %s", strings.Join(strs[:len(strs)-1], "_"))
}
