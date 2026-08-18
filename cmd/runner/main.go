package main

import (
	"fmt"
	"os"
)

const version = "dev"

func main() {
	if _, err := fmt.Fprintf(os.Stdout, "runner version=%s\n", version); err != nil {
		os.Exit(1)
	}
}
