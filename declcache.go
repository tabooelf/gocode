package main

import (
	"os"
	"go/ast"
	"go/parser"
	"io/ioutil"
	"go/token"
	"path"
	"fmt"
)

//-------------------------------------------------------------------------
// ModuleImport
// Contains import information from a single file
//-------------------------------------------------------------------------

type ModuleImport struct {
	Alias  string
	Name   string
	Path   string
	Module *Decl
}

type ModuleImports []ModuleImport

func NewModuleImports(filename string, decls []ast.Decl) ModuleImports {
	mi := make(ModuleImports, 0, 16)
	mi.appendImports(filename, decls)
	return mi
}

func (mi *ModuleImports) appendImports(filename string, decls []ast.Decl) {
	for _, decl := range decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				imp := spec.(*ast.ImportSpec)
				path, alias := pathAndAlias(imp)
				fullpath := fullPathForModule(filename, path)
				mi.appendImport(alias, path, fullpath)
			}
		} else {
			return
		}
	}
}

func (mi *ModuleImports) appendImport(alias, name, path string) {
	v := *mi
	if alias == "_" || alias == "." {
		// TODO: support for modules imported in the current namespace
		return
	}
	if name[0] == '.' {
		// use file path for local packages as name
		name = path
	}

	n := len(v)
	if cap(v) < n+1 {
		s := make(ModuleImports, n, n*2+1)
		copy(s, v)
		v = s
	}

	v = v[0 : n+1]
	v[n] = ModuleImport{alias, name, path, nil}
	*mi = v
}

//-------------------------------------------------------------------------
// DeclFileCache
// Contains cache for top-level declarations of a file. Used in both 
// autocompletion and refactoring utilities.
//-------------------------------------------------------------------------

type DeclFileCache struct {
	name  string // file name
	mtime int64  // last modification time

	Data      []byte           // file contents
	File      *ast.File        // an AST tree
	Decls     map[string]*Decl // top-level declarations
	Error     os.Error         // last error
	Modules   ModuleImports    // import information
	FileScope *Scope
}

func NewDeclFileCache(name string) *DeclFileCache {
	f := new(DeclFileCache)
	f.name = name
	return f
}

func (f *DeclFileCache) update() {
	stat, err := os.Stat(f.name)
	if err != nil {
		f.File = nil
		f.Data = nil
		f.Decls = nil
		f.Error = err
		return
	}

	if f.mtime == stat.Mtime_ns {
		return
	}

	f.mtime = stat.Mtime_ns
	f.readFile(f.name)
}

func (f *DeclFileCache) readFile(filename string) {
	f.Data, f.Error = ioutil.ReadFile(f.name)
	if f.Error != nil {
		return
	}

	f.File, f.Error = parser.ParseFile("", f.Data, 0)
	f.FileScope = NewScope(nil)
	f.Modules = NewModuleImports(f.name, f.File.Decls)
	f.Decls = make(map[string]*Decl, len(f.File.Decls))
	for _, decl := range f.File.Decls {
		appendToTopDecls(f.Decls, decl, f.FileScope)
	}
}

func (f *DeclFileCache) applyImports() {
	f.FileScope.entities = make(map[string]*Decl, len(f.Modules))
	for _, mi := range f.Modules {
		f.FileScope.addDecl(mi.Alias, mi.Module)
	}
}

func appendToTopDecls(decls map[string]*Decl, decl ast.Decl, scope *Scope) {
	foreachDecl(decl, func(decl ast.Decl, name *ast.Ident, value ast.Expr, valueindex int) {
		d := NewDeclFromAstDecl(name.Name, 0, decl, value, valueindex, scope)
		if d == nil {
			return
		}

		methodof := MethodOf(decl)
		if methodof != "" {
			decl, ok := decls[methodof]
			if ok {
				decl.AddChild(d)
			} else {
				decl = NewDecl(methodof, DECL_METHODS_STUB, scope)
				decls[methodof] = decl
				decl.AddChild(d)
			}
		} else {
			decl, ok := decls[d.Name]
			if ok {
				decl.ExpandOrReplace(d)
			} else {
				decls[d.Name] = d
			}
		}
	})
}

func fullPathForModule(filename, p string) string {
	if p[0] == '.' {
		dir, _ := path.Split(filename)
		return fmt.Sprintf("%s.a", path.Join(dir, p))
	}
	return findGlobalFile(p)
}

func pathAndAlias(imp *ast.ImportSpec) (string, string) {
	path := string(imp.Path.Value)
	alias := ""
	if imp.Name != nil {
		alias = imp.Name.Name
	}
	path = path[1 : len(path)-1]
	return path, alias
}

func findGlobalFile(imp string) string {
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")

	pkgdir := fmt.Sprintf("%s_%s", goos, goarch)
	pkgfile := fmt.Sprintf("%s.a", imp)

	return path.Join(goroot, "pkg", pkgdir, pkgfile)
}

func packageName(file *ast.File) string {
	if file.Name != nil {
		return file.Name.Name
	}
	return ""
}

//-------------------------------------------------------------------------
// DeclCache
//-------------------------------------------------------------------------

type DeclCache struct {
	cache map[string]*DeclFileCache
}

func NewDeclCache() *DeclCache {
	return &DeclCache{make(map[string]*DeclFileCache)}
}

func (c *DeclCache) Get(filename string) *DeclFileCache {
	f, ok := c.cache[filename]
	if !ok {
		f = NewDeclFileCache(filename)
		c.cache[filename] = f
	}
	f.update()
	return f
}