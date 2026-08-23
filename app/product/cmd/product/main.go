package main

import (
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
)

var Version string

func main() {
	platformruntime.Run(platformruntime.Spec{
		Name: "eagle.product", Version: Version, Build: buildApp,
		Requirements: config.Requirements{AuthorizationUpstream: true, Redis: true, ServiceAuth: true},
	})
}
