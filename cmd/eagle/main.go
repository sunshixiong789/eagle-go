package main

import platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"

var Version string

func main() {
	platformruntime.Run(platformruntime.Spec{
		Name: "eagle", Version: Version, Build: composeApp,
	})
}
