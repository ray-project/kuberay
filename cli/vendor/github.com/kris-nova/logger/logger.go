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
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type LoggerFunc func(format string, a ...interface{})

const (
	// [Log Constants]
	//
	// These are the bitwise values for the
	// various log options.
	//
	// Also these are the string prefixes
	// for the log lines.
	LogAlways     = 1
	PreAlways     = "Always    "
	LogSuccess    = 2
	PreSuccess    = "Success   "
	LogCritical   = 4
	PreCritical   = "Critical  "
	LogWarning    = 8
	PreWarning    = "Warning   "
	LogInfo       = 16
	PreInfo       = "Info      "
	LogDebug      = 32
	PreDebug      = "Debug     "
	LogDeprecated = 64
	PreDeprecated = "Deprecated"

	LogLegacyLevel2           = LogAlways | LogSuccess | LogCritical | LogWarning | LogInfo
	LogLegacyLevel2Deprecated = LogLegacyLevel2 | LogDeprecated

	// Enable all Logging levels
	// [127]
	LogEverything = LogAlways | LogSuccess | LogDebug | LogInfo | LogWarning | LogCritical | LogDeprecated
)

type WriterMode int

var (

	// BitwiseLevel is the preferred
	// way of managing log levels.
	//
	// ----- [ Bitwise Chart ] ------
	//
	// LogEverything (All Levels)
	// LogAlways
	// LogSuccess
	// LogCritical
	// LogWarning
	// LogInfo
	// LogDebug
	// LogDeprecated
	//
	// TODO @kris-nova In the next release flip to LogEverything
	// BitwiseLevel = LogEverything
	BitwiseLevel = LogLegacyLevel2Deprecated

	// A custom io.Writer to use regardless of Mode
	Writer io.Writer = os.Stdout

	// Layout is the time layout string to use
	Layout string = time.RFC3339
)

// LineBytes will format a log line, and return
// a slice of bytes []byte
func LineBytes(prefix, format string, a ...interface{}) []byte {
	return []byte(Line(prefix, format, a...))
}

// Line will format a log line, and return a string
var Line = func(prefix, format string, a ...interface{}) string {
	if !strings.Contains(format, "\n") {
		format = fmt.Sprintf("%s%s", format, "\n")
	}
	if Timestamps {
		now := time.Now()
		fNow := now.Format(Layout)
		prefix = fmt.Sprintf("%s [%s]", fNow, prefix)
	} else {
		prefix = fmt.Sprintf("[%s]", prefix)
	}
	return fmt.Sprintf("%s  %s", prefix, fmt.Sprintf(format, a...))
}

// Always
func Always(format string, a ...interface{}) {
	d()
	a = legacyFindWriter(a...)
	if BitwiseLevel&LogAlways != 0 {
		fmt.Fprint(Writer, Line(PreAlways, format, a...))
	}
}

// Success
func Success(format string, a ...interface{}) {
	d()
	a = legacyFindWriter(a...)
	if BitwiseLevel&LogSuccess != 0 {
		fmt.Fprint(Writer, Line(PreSuccess, format, a...))
	}
}

// Debug
func Debug(format string, a ...interface{}) {
	d()
	a = legacyFindWriter(a...)
	if BitwiseLevel&LogDebug != 0 {
		fmt.Fprint(Writer, Line(PreDebug, format, a...))
	}
}

// Info
func Info(format string, a ...interface{}) {
	d()
	a = legacyFindWriter(a...)
	if BitwiseLevel&LogInfo != 0 {
		fmt.Fprint(Writer, Line(PreInfo, format, a...))
	}
}

// Warning
func Warning(format string, a ...interface{}) {
	d()
	a = legacyFindWriter(a...)
	if BitwiseLevel&LogWarning != 0 {
		fmt.Fprint(Writer, Line(PreWarning, format, a...))
	}
}

// Critical
func Critical(format string, a ...interface{}) {
	d()
	a = legacyFindWriter(a...)
	if BitwiseLevel&LogCritical != 0 {
		fmt.Fprint(Writer, Line(PreCritical, format, a...))
	}
}

// Used to show deprecated log lines
func Deprecated(format string, a ...interface{}) {
	a = legacyFindWriter(a...)
	if BitwiseLevel&LogDeprecated != 0 {
		fmt.Fprint(Writer, Line(PreDeprecated, format, a...))
	}
}

// d is used by every function
// and is an easy way to add
// global logic/state to the logger
func d() {
	checkDeprecatedValues()
}
