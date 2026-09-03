package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cubeship %s\n", version)
		os.Exit(0)
	}
	fmt.Println("cubeship: no command implemented yet")
}
