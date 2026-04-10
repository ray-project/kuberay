package main

import (
	"fmt"
	"os"
)

func main() {
	cmd := NewHistoryServerCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
