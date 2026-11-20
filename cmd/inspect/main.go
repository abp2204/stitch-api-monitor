package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/abp2204/stitch-api-monitor/inspection"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(paths []string) error {
	if len(paths) != 1 {
		return errors.New("usage: go run ./cmd/inspect FILE.ts")
	}
	switch strings.ToLower(filepath.Ext(paths[0])) {
	case ".js", ".ts", ".mjs", ".cjs":
	default:
		return errors.New("provide a JavaScript or TypeScript source file")
	}
	file, err := os.Open(paths[0])
	if err != nil {
		return err
	}
	defer file.Close()
	const maxSize = 150000
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return err
	}
	if len(data) > maxSize {
		return fmt.Errorf("source exceeds %d bytes", maxSize)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(inspection.InvoiceCalls(string(data)))
}
