package main

import (
	"fmt"
	"os"

	"github.com/Akkitto/template-wizard/internal/app"
)

var version = "dev"

func main() {
	if err := app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
