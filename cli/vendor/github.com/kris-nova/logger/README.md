# Kris Nóva Logger

## History

- [2017] Originally ported from it's [original location](https://github.com/kubicorn/kubicorn/tree/master/pkg/logger) in the Kubicorn code base.
- [2021] Refactored to support custom `io.Writer`'s

<center><img src="/screenshot.png"></center>


## Install

```bash 
go get github.com/kris-nova/logger
```

## Basic Usage

```go
package main

import (
	"github.com/kris-nova/logger"
	"os"
)

func main() {
	// Options
	logger.Writer = os.Stdout // This is not needed
	logger.BitwiseLevel = logger.LogCritical | logger.LogWarning // Customize your levels
	logger.BitwiseLevel = logger.LogEverything // Turn everything on
	logger.BitwiseLevel = logger.LogAlways // Only log Always()
	logger.BitwiseLevel = logger.LogEverything // Turn everything back on 
	// 
	
	// Log lines
	logger.Debug("Check this out %d", 123)
	logger.Info("Cool!")
	logger.Success("Hooray!")
	logger.Always("Hello!")
	logger.Critical("Oh No!")
	logger.Warning("Beware...")
	logger.Deprecated("Don't do this!")
	//
}

```


## Rainbow logs

```go

package main

import (
	"github.com/kris-nova/logger"
	lol "github.com/kris-nova/lolgopher"
)


func main(){
	//
	logger.Writer = lol.NewLolWriter()          // Sometimes this will work better
	logger.Writer = lol.NewTruecolorLolWriter() // Comment one of these out
	//

	logger.BitwiseLevel = logger.LogEverything
	logger.Always("Rainbow logging")
	logger.Always("Rainbow logging")
	logger.Always("Rainbow logging")
	logger.Always("Rainbow logging")
}

```