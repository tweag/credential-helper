package main

import (
	"fmt"
	"os"
	"path"

	"github.com/bazelbuild/rules_go/go/runfiles"
	"github.com/tweag/credential-helper/agent/locate"
)

var credentialHelperBin = "not set" // Set at link time

func main() {
	path, err := runfiles.Rlocation(credentialHelperBin)
	if err != nil {
		fatalFmt("Failed to find %s: %v", credentialHelperBin, err)
	}

	if _, err := os.Stat(path); err != nil {
		fatalFmt("Failed to stat %s: %v", path, err)
	}
	destination, err := install(path)
	if err != nil {
		fatalFmt("Failed to install %s: %v", path, err)
	}
	fmt.Println(destination)
}

func install(credentialHelperBin string) (string, error) {
	destination, err := locate.CredentialHelper()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path.Dir(destination), 0o755); err != nil {
		return "", err
	}
	_ = os.Remove(destination)
	return destination, os.Link(credentialHelperBin, destination)
}

func fatalFmt(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
