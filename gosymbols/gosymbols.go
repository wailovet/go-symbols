// The gosymbols command prints type information for package-level symbols.
package gosymbols

import (
	"encoding/json"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Package   string `json:"package"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

var mutex sync.Mutex
var syms = make([]symbol, 0)

type visitor struct {
	pkg   *ast.Package
	fset  *token.FileSet
	query string
	syms  []symbol
}

func (v *visitor) Visit(node ast.Node) bool {
	descend := true

	var ident *ast.Ident
	var kind string
	switch t := node.(type) {
	case *ast.FuncDecl:
		kind = "func"
		ident = t.Name
		descend = false

	case *ast.TypeSpec:
		kind = "type"
		ident = t.Name
		descend = false
		if _, ok := t.Type.(*ast.InterfaceType); ok {
			kind = "interface"
		}
	}

	if ident != nil && strings.Contains(strings.ToLower(ident.Name), v.query) {
		f := v.fset.File(ident.Pos())

		v.syms = append(v.syms, symbol{
			Package: v.pkg.Name,
			Path:    f.Name(),
			Name:    ident.Name,
			Kind:    kind,
			Line:    f.Line(ident.Pos()) - 1,
		})
	}

	return descend
}

var haveSrcDir = true

func forEachPackage(ctxt *build.Context, found func(importPath string, err error)) {
	// We use a counting semaphore to limit
	// the number of parallel calls to ReadDir.
	sema := make(chan bool, 20)

	ch := make(chan item)

	var srcDirs []string
	if haveSrcDir {
		srcDirs = ctxt.SrcDirs()
	} else {
		srcDirs = append(srcDirs, ctxt.GOPATH)
	}

	var wg sync.WaitGroup
	for _, root := range srcDirs {
		root := root
		wg.Add(1)
		go func() {
			allPackages(ctxt, sema, root, ch)
			wg.Done()
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

	// All calls to found occur in the caller's goroutine.
	for i := range ch {
		found(i.importPath, i.err)
	}
}

type item struct {
	importPath string
	err        error // (optional)
}

func allPackages(ctxt *build.Context, sema chan bool, root string, ch chan<- item) {
	root = filepath.Clean(root) + string(os.PathSeparator)

	var wg sync.WaitGroup

	var walkDir func(dir string)
	walkDir = func(dir string) {
		// Avoid .foo, _foo, and testdata directory trees.
		base := filepath.Base(dir)
		if base == "" || base[0] == '.' || base[0] == '_' || base == "testdata" {
			return
		}

		pkg := filepath.ToSlash(strings.TrimPrefix(dir, root))

		// Prune search if we encounter any of these import paths.
		switch pkg {
		case "builtin":
			return
		}

		sema <- true
		files, err := ioutil.ReadDir(dir)
		<-sema
		if pkg != "" || err != nil {
			ch <- item{pkg, err}
		}
		for _, fi := range files {
			fi := fi
			if fi.IsDir() {
				wg.Add(1)
				go func() {
					walkDir(filepath.Join(dir, fi.Name()))
					wg.Done()
				}()
			}
		}
	}

	walkDir(root)
	wg.Wait()
}

func DoCoreMain(dir string, query string) string {
	syms = make([]symbol, 0)
	ctxt := build.Default // copy
	ctxt.GOPATH = dir     // disable GOPATH
	ctxt.GOROOT = ""

	fset := token.NewFileSet()
	sema := make(chan int, 8) // concurrency-limiting semaphore
	var wg sync.WaitGroup

	if _, err := os.Stat(filepath.Join(dir, "src")); err != nil {
		haveSrcDir = false
	}

	var all = 0
	var count = 0

	// Here we can't use buildutil.ForEachPackage here since it only considers
	// src dirs and this tool should be able to run against a golang source dir.
	forEachPackage(&ctxt, func(path string, err error) {
		if path == "" {
			return
		}
		all++
		wg.Add(1)
		go func() {
			sema <- 1 // acquire token
			defer func() {
				count++
				Schedule(count, all)
				<-sema // release token
			}()

			v := &visitor{
				fset:  fset,
				query: query,
			}
			defer func() {
				mutex.Lock()
				syms = append(syms, v.syms...)
				mutex.Unlock()
			}()

			defer wg.Done()

			if haveSrcDir {
				path = filepath.Join(dir, "src", path)
			} else {
				path = filepath.Join(dir, path)
			}

			parsed, _ := parser.ParseDir(fset, path, nil, 0)
			// Ignore any errors, they are irrelevant for symbol search.

			for _, astpkg := range parsed {
				v.pkg = astpkg
				for _, f := range astpkg.Files {
					ast.Inspect(f, v.Visit)
				}
			}
		}()
	})
	wg.Wait()

	b, _ := json.MarshalIndent(syms, "", " ")
	return string(b)
}

var Schedule = func(i int, count int) {

}
