// Package logs is
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
package ray

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestTrim(t *testing.T) {
	absoluteLogPathName := " /tmp/ray/test/LLogs/events///aa/a.txt  "
	logdir := "/tmp/ray/test/lLogs/"

	absoluteLogPathName = strings.TrimSpace(absoluteLogPathName)
	absoluteLogPathName = filepath.Clean(absoluteLogPathName)

	logdir = strings.TrimSpace(logdir)
	logdir = filepath.Clean(logdir)

	relativePath := strings.TrimPrefix(absoluteLogPathName, logdir+"/")
	//relativePath := strings.TrimPrefix(absoluteLogPathName, logdir)
	// 分割相对路径为子目录和文件名
	subdir, filename := filepath.Split(relativePath)
	test_path_join := path.Join("aa./b/c/d", "e")
	t.Logf("file [%s] logdir [%s] subdir %s filename %s", absoluteLogPathName,
		logdir, subdir, filename)
	t.Logf("test_path_join [%s]", test_path_join)
}

func TestWalk(t *testing.T) {
	watchPath := "/tmp/ray/test/LLogs/"
	filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logrus.Errorf("Walk path error %v", err)
			return err // 返回错误
		}
		// 检查是否是文件
		if !info.IsDir() {
			logrus.Infof("Find new file %s", path) // 输出文件路径
		}
		return nil
	})
}
