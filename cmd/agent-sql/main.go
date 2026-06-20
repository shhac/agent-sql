package main

import (
	"github.com/shhac/agent-sql/internal/cli"
)

var version = "dev"

func main() {
	cli.Run(version)
}
