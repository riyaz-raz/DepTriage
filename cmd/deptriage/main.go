// Command deptriage is the entry point for the deptriage CLI.
package main

import "github.com/deptriage/deptriage/internal/cli"

var version = "dev"

func main() {
	cli.Execute(version)
}
