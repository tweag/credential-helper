package main

import (
	"context"
	"os"

	"github.com/tweag/credential-helper/cmd/root"
)

func main() {
	root.Run(context.Background(), helperFactory, newCache, os.Args)
}
