// The gosymbols command prints type information for package-level symbols.
package main

import (
	"flag"
	"fmt"
	"go/build"
	"os"
	"strings"

	"github.com/wailovet/go-symbols/gosymbols"
	"golang.org/x/tools/go/buildutil"
)

const usage = `Usage: gosymbols <package> ...`

func init() {
	flag.Var((*buildutil.TagsFlag)(&build.Default.BuildTags), "tags", buildutil.TagsFlagDoc)
}

func main() {

	flag.Parse()

	args := flag.Args()

	if len(args) < 1 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	dir := args[0]

	var query string
	if len(args) > 1 {
		query = args[1]
	}
	query = strings.ToLower(query)

	gosymbols.Schedule = func(i, count int) {
		fmt.Println("进度:", i, count)
	}
	json := gosymbols.DoCoreMain(dir, query)
	fmt.Print(json)
}
