package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args)%2 == 0 || len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: check_file_hash <filename> <expected sha256>")
		os.Exit(1)
	}

	args := os.Args[1:]

	for i := 0; i < len(args); i += 2 {
		filename := args[i]
		expectedHash := args[i+1]

		if err := checkExpectedHash(filename, expectedHash); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	}
}

func checkExpectedHash(path string, expectedHash string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file %s: %v", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hashing file %s: %v", path, err)
	}

	got := fmt.Sprintf("%x", hash.Sum(nil))

	if got != expectedHash {
		return fmt.Errorf("hash of file %s does not match expected hash: expected %s, got %s", path, expectedHash, got)
	}

	return nil
}
