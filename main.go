package main

import (
	"fmt"
	"os"

	"github.com/mblarsen/unlearn/cmd/unlearn"
)

func main() {
	if err := unlearn.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
