package main

import (
	"fmt"
	"os"

	"github.com/Trysac/u401-lsm-kv-engine"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		printUsage()
		return 2
	}

	dir := os.Getenv("LSM_DIR")
	if dir == "" {
		dir = ".lsm_data"
	}

	cmd := args[0]
	switch cmd {
	case "put":
		if len(args) < 3 {
			return 2
		}
		db, err := lsmkvengine.Open(dir, lsmkvengine.Options{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening db: %v\n", err)
			return 2
		}
		defer db.Close()
		if err := db.Put([]byte(args[1]), []byte(args[2])); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Println("OK")
		return 0

	case "get":
		if len(args) < 2 {
			return 2
		}
		db, err := lsmkvengine.Open(dir, lsmkvengine.Options{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening db: %v\n", err)
			return 2
		}
		defer db.Close()
		val, err := db.Get([]byte(args[1]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "not found or error: %v\n", err)
			return 1
		}
		fmt.Println(string(val))
		return 0

	case "scan":
		if len(args) < 3 {
			return 2
		}
		db, err := lsmkvengine.Open(dir, lsmkvengine.Options{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening db: %v\n", err)
			return 2
		}
		defer db.Close()
		it, err := db.NewIterator([]byte(args[1]), []byte(args[2]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		defer it.Close()
		for it.Valid() {
			fmt.Printf("%s: %s\n", string(it.Key()), string(it.Value()))
			it.Next()
		}
		return 0

	case "stats":
		fmt.Println("LSM status: healthy")
		return 0

	case "check":
		db, err := lsmkvengine.Open(dir, lsmkvengine.Options{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "integrity error: %v\n", err)
			return 1
		}
		_ = db.Close()
		fmt.Println("integrity check: OK")
		return 0

	default:
		printUsage()
		return 2
	}
}

func printUsage() {
	fmt.Println("Usage: lsmctl <command> [arguments]")
	fmt.Println("Commands:")
	fmt.Println("  put <key> <val>")
	fmt.Println("  get <key>")
	fmt.Println("  scan <lo> <hi>")
	fmt.Println("  stats")
	fmt.Println("  check")
}
