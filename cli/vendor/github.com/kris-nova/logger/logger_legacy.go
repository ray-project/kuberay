// Copyright © 2021 Kris Nóva <kris@nivenly.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logger

import (
	"io"
	"sync"
)

// Legacy Logic for 0.1.0
//
// Here we store the legacy (0.1.0) compatible configuration options
// that will eventually be deprecated.
//
var (

	// Timestamps are used to toggle timestamps
	// deprecated
	Timestamps = true

	// TestMode is used for running
	// the regression tests.
	// deprecated
	TestMode = false

	// Color is no longer used
	// deprecated
	Color = true

	// Level is the legacy log level
	//
	// 0 (Least verbose)
	// 1
	// 2
	// 3
	// 4 (Most verbose)
	//
	// deprecated
	Level = -1
)

var (
	testRaceMutex = sync.Mutex{}
	annoyed       = false
)

// checkDeprecatedValues is a singleton
// that will only execute once.
// This will convert the legacy logger.Level
// to the new logger.BitwiseLevel
//
//	LogEverything =
func checkDeprecatedValues() {
	testRaceMutex.Lock()
	defer testRaceMutex.Unlock()
	if Level != -1 {
		if !annoyed {
			Deprecated("********")
			Deprecated("***")
			Deprecated("*")
			Deprecated("logger.Level is deprecated. Use logger.BitwiseLevel")
			Deprecated("*")
			Deprecated("***")
			Deprecated("********")
			annoyed = true
		}
		if Level == 4 {
			BitwiseLevel = LogDeprecated | LogAlways | LogSuccess | LogCritical | LogWarning | LogInfo | LogDebug
		} else if Level == 3 {
			BitwiseLevel = LogDeprecated | LogAlways | LogSuccess | LogCritical | LogWarning | LogInfo
		} else if Level == 2 {
			BitwiseLevel = LogDeprecated | LogAlways | LogSuccess | LogCritical | LogWarning
		} else if Level == 1 {
			BitwiseLevel = LogDeprecated | LogAlways | LogSuccess | LogCritical
		} else if Level == 0 {
			BitwiseLevel = LogDeprecated | LogAlways | LogSuccess
		} else {
			BitwiseLevel = LogDeprecated | LogEverything
		}
	}
}

// legacyFindWriter will check if there is an io.Writer
// appended to the end of the arguments passed to the logger.
//
// deprecated
func legacyFindWriter(a ...interface{}) []interface{} {
	if n := len(a); n > 0 {
		// extract an io.Writer at the end of a
		if newWriter, ok := a[n-1].(io.Writer); ok {
			Writer = newWriter
			a = a[0 : n-1]
		}
	}
	return a
}
