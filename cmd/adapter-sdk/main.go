package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: adapter-sdk <build|preflight> ...")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "not implemented: %s\n", os.Args[1])
	os.Exit(1)
}
