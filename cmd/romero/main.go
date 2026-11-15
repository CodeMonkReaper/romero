package main

import (
	"fmt"
	"io"
	"os"

	"github.com/romero-project/romero/internal/interfaces/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	cmd := cli.NewRootCmd(cli.DefaultDependencies())
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
