// Package utils is
/*
Copyright 2024 by the zhangjie bingyu.zj@alibaba-inc.com Authors.

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
package utils

type ClusterInfo struct {
	Name            string `json:"name"`
	SessionName     string `json:"sessionName"`
	CreateTime      string `json:"createTime"`
	CreateTimeStamp int64  `json:"createTimeStamp"`
}

type ClusterInfoList []ClusterInfo

func (a ClusterInfoList) Len() int           { return len(a) }
func (a ClusterInfoList) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ClusterInfoList) Less(i, j int) bool { return a[i].CreateTimeStamp > a[j].CreateTimeStamp } // 降序排序
