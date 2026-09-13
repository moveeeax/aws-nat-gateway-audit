package main

import (
	"os"

	"github.com/moveeeax/aws-nat-gateway-audit/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
