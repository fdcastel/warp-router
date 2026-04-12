package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: warp <command> [options]")
		fmt.Fprintln(os.Stderr, "Commands: validate, apply, status, rollback")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "validate":
		fmt.Println("validate: not yet implemented")
	case "apply":
		fmt.Println("apply: not yet implemented")
	case "status":
		fmt.Println("status: not yet implemented")
	case "rollback":
		fmt.Println("rollback: not yet implemented")
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
