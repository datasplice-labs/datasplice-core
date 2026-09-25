package main

import (
	"github.com/datasplice-labs/datasplice-core/cmd"
	"github.com/datasplice-labs/datasplice-core/version"
)

func main() {
	cmd.Execute(version.GetVersion())
}
