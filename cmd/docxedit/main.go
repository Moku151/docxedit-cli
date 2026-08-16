package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"docxedit/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "docxedit: %v\n", err)
		os.Exit(1)
	}
}
