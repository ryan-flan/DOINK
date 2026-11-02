// Command doink watches DOINK's SavedVariables file and posts new events
// to Discord.
package main

import "fmt"

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	fmt.Println("DOINK companion v" + version)
}
