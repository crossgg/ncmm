package main

import (
	_ "embed"
	"strings"

	"github.com/3899/ncmm/internal/ncmm"
)

//go:embed VERSION
var versionStr string

var (
	Version   string
	Commit    = "none"
	BuildTime = "now"
)

func init() {
	Version = strings.TrimSpace(versionStr)
}

func main() {
	c := ncmm.New()
	c.Version(Version, BuildTime, Commit)
	c.Execute()
}
