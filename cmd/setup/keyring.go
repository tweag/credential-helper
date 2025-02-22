package setup

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tweag/credential-helper/agent/locate"
	keyring "github.com/zalando/go-keyring"
)

// KeyringProcess is the entry point for the setup-keyring command.
// It takes a string key (name of the secret) and a string value (the secret itself) and stores it in the keyring.
func KeyringProcess(args []string) {
	var sourceFilePath string
	var read bool
	var clear bool

	flagSet := flag.NewFlagSet("setup-keyring", flag.ExitOnError)
	flagSet.Usage = func() {
		fmt.Fprintf(flagSet.Output(), "Stores a secret read from a file or stdin in the system keyring under the name specified by service.\n\n")
		fmt.Fprintf(flagSet.Output(), "Usage: credential-helper setup-keyring [--file file] [--read | --clear] [service]\n")
		flagSet.PrintDefaults()
		examples := []string{
			"credential-helper setup-keyring gh:github.com < secret.txt",
			"credential-helper setup-keyring --clear gh:github.com",
			"credential-helper setup-keyring --file secret.txt tweag-credential-helper:remoteapis",
			"credential-helper setup-keyring --read tweag-credential-helper:buildbuddy_api_key",
		}
		fmt.Fprintf(flagSet.Output(), "\nExamples:\n")
		for _, example := range examples {
			fmt.Fprintf(flagSet.Output(), "  $ %s\n", example)
		}
		os.Exit(1)
	}
	flagSet.StringVar(&sourceFilePath, "file", "", "File to read the secret from")
	flagSet.BoolVar(&read, "read", false, "Print the current secret stored in the keyring for this service to stdout and exit")
	flagSet.BoolVar(&clear, "clear", false, "Clear the secret stored in the keyring for this service and exit")

	if err := flagSet.Parse(args); err != nil {
		fatalFmt("parsing flags for setup-keyring: %v", err)
	}

	if flagSet.NArg() != 1 {
		flagSet.Usage()
	}

	service := flagSet.Arg(0)

	if read && clear {
		fatalFmt("cannot specify both --read and --clear")
	}
	if read {
		secret, err := keyring.Get(service, "")
		if err != nil {
			fatalFmt("reading secret from keyring: %v", err)
		}
		fmt.Print(secret)
		return
	}
	if clear {
		if err := keyring.Delete(service, ""); err != nil {
			fatalFmt("deleting secret from keyring: %v", err)
		}
		fmt.Printf("Cleared secret %s\n", service)
		return
	}

	var sourceFile io.Reader
	if len(sourceFilePath) > 0 {
		// the credential-helper process changes it's own working directory
		// during setup.
		// This remapping is necessary to find original, relative paths.
		sourceFilePath = locate.RemapToOriginalWorkingDirectory(sourceFilePath)
		var openErr error
		sourceFile, openErr = os.OpenFile(sourceFilePath, os.O_RDONLY, 0)
		if openErr != nil {
			fatalFmt("opening source file %s: %v", sourceFilePath, openErr)
		}
	} else {
		// use stdin as source
		sourceFile = os.Stdin
	}

	secret, err := io.ReadAll(sourceFile)
	if err != nil {
		sourceName := sourceFilePath
		if sourceName == "" {
			sourceName = "stdin"
			fmt.Fprintf(os.Stderr, "Reading secret from stdin.\n")
		}
		fatalFmt("reading secret from %s: %v", sourceName, err)
	}

	if err := keyring.Set(service, "", string(secret)); err != nil {
		fatalFmt("storing secret in keyring: %v", err)
	}

	fmt.Printf("Stored secret %s\n", service)
}

func fatalFmt(format string, args ...any) {
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
