package logging

import "github.com/sirupsen/logrus"

// Init configures the shared logrus logger used by historyserver binaries.
func Init() {
	logrus.SetReportCaller(true)
}
