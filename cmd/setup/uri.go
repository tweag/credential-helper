package setup

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/cmd/internal/util"
	"github.com/tweag/credential-helper/config"
	"github.com/tweag/credential-helper/logging"
)

// URIProcess is the entry point for the setup-uri command.
func URIProcess(args []string, helperFactory api.HelperFactory, configReader config.ConfigReader) {
	ctx := context.Background()

	flagSet := flag.NewFlagSet("setup-uri", flag.ExitOnError)
	flagSet.Usage = func() {
		fmt.Fprintf(flagSet.Output(), "Prints setup instructions for a given uri.\n\n")
		fmt.Fprintf(flagSet.Output(), "Usage: credential-helper setup-uri [uri]\n")
		flagSet.PrintDefaults()
		examples := []string{
			"credential-helper setup-uri https://github.com/my-org/project/releases/download/v1.2.3/my-artifact.tar.gz",
			"credential-helper setup-uri https://raw.githubusercontent.com/my-org/project/6012...a5de28/file.txt",
			"credential-helper setup-uri https://storage.googleapis.com/bucket/path/to/object",
			"credential-helper setup-uri https://my-bucket.s3.amazonaws.com/path/to/object",
			"credential-helper setup-uri https://org-id.r2.cloudflarestorage.com/bucket/path/to/object",
			"credential-helper setup-uri https://index.docker.io/v2/library/hello-world/blobs/sha256:d2c94e...7264ac5a",
		}
		fmt.Fprintf(flagSet.Output(), "\nExamples:\n")
		for _, example := range examples {
			fmt.Fprintf(flagSet.Output(), "  $ %s\n", example)
		}
		os.Exit(1)
	}

	if err := flagSet.Parse(args); err != nil {
		fatalFmt("parsing flags for setup-uri: %v", err)
	}

	if flagSet.NArg() != 1 {
		flagSet.Usage()
	}

	uri := flagSet.Arg(0)

	ctx, authenticator := util.Configure(ctx, helperFactory, configReader, uri)

	var instructionGiver api.URISetupper

	// first try to use the authenticator directly
	instructionGiver, ok := authenticator.(api.URISetupper)

	// as a fallback, try to use the resolver
	if !ok {
		resolver, err := authenticator.Resolver(ctx)
		if err != nil {
			logging.Fatalf("instantiating resolver: %s", err)
		}

		// check if either the resolver or the authenticator provides setup instructions
		if resolverInstructionGiver, ok := resolver.(api.URISetupper); ok {
			instructionGiver = resolverInstructionGiver
		}
	}

	if !ok {
		fmt.Printf("No setup instructions available for %s\nMaybe the config file is missing or incorrectly configured?", uri)
		os.Exit(1)
	}

	setupInstructions := instructionGiver.SetupInstructionsForURI(ctx, uri)
	fmt.Println(setupInstructions)
}
