package main

import (
	"fmt"
	"os"
)

func main() {
	cmd := NewCollectorCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
