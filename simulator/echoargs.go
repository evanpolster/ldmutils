package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args
	// skip the first arg which is the program itself
	if len(args) == 1 {
		fmt.Println("No command line arguments are passed to simulated mailx")
		return
	}
	for i, v := range args {
		if i == 0 {
			continue
		}
		fmt.Printf("%#v\n",v)
	}
}
