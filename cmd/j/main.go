package main

import (
	"os"

	"github.com/spacelions/j/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
