package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, out io.Writer) int {
	if len(args) > 0 && args[0] == "version" {
		fmt.Fprintln(out, "romero v0.1.0-phase01")
		return 0
	}
	return 0
}
