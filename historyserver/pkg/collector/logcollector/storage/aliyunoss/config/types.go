//  Package config is
/*
Copyright 2024 by the bingyu bingyu.zj@alibaba-inc.com Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"k8s.io/apimachinery/pkg/util/sets"
)

type EnterConfig struct {
	Module             string
	ModuleExecBinary   string
	EnterOssExpireHour int
	WatchLogDir        string
	EnableMeta         bool
}

type GlobalConfig struct {
	OSSEndpoint         string
	OSSRegion           string
	OSSBucket           string
	OSSHistoryServerDir string
}

type RayMetaHanderConfig struct {
	GlobalConfig
	RayClusterName string
	RayClusterID   string
	OSSExpireHour  int
}

type RayHistoryServerConfig struct {
	GlobalConfig
	DashBoardDir string

	ArmsRegionId     string
	ACKClusterId     string
	WebAppId         string
	WebAppSecret     string
	LocalServiceName string
	AllowedUID       sets.Set[string]
	AllowedAID       sets.Set[string]

	MaxOssListLimit int
}
