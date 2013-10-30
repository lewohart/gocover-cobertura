package main

import (
	"encoding/xml"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	convert(os.Stdin, os.Stdout)
}

func convert(in io.Reader, out io.Writer) {
	profiles, err := ParseProfiles(in)
	if err != nil {
		panic("Can't parse profiles")
	}

	srcDirs := build.Default.SrcDirs()
	sources := make([]Source, len(srcDirs))
	for i, dir := range srcDirs {
		sources[i] = Source{dir}
	}

	coverage := Coverage{Sources: sources, Packages: nil, Timestamp: time.Now().UnixNano() / int64(time.Millisecond)}
	coverage.parseProfiles(profiles)

	fmt.Fprintf(out, xml.Header)
	fmt.Fprintf(out, "<!DOCTYPE coverage SYSTEM \"http://cobertura.sourceforge.net/xml/coverage-03.dtd\">\n")

	encoder := xml.NewEncoder(out)
	encoder.Indent("", "\t")
	err = encoder.Encode(coverage)
	if err != nil {
		panic(err)
	}

	fmt.Fprintln(out)
}

func (cov *Coverage) parseProfiles(profiles []*Profile) error {
	cov.Packages = []Package{}
	for _, profile := range profiles {
		cov.parseFile(profile.FileName)
	}
	return nil
}

func (cov *Coverage) parseFile(fileName string) error {
	absFilePath, err := findFile(fileName)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, absFilePath, nil, 0)
	if err != nil {
		return err
	}
	data, err := ioutil.ReadFile(absFilePath)
	if err != nil {
		return err
	}

	pkgPath, _ := filepath.Split(fileName)
	pkgPath = strings.TrimRight(pkgPath, string(os.PathSeparator))

	var pkg *Package
	for _, p := range cov.Packages {
		if p.Name == pkgPath {
			pkg = &p
		}
	}
	if pkg == nil {
		pkg = &Package{Name: pkgPath, Classes: []Class{}}
	}
	visitor := &fileVisitor{
		fset:     fset,
		name:     fileName,
		astFile:  parsed,
		coverage: cov,
		classes:  make(map[string]*Class),
		pkg:      pkg,
		data:     data,
	}
	ast.Walk(visitor, visitor.astFile)
	for _, c := range visitor.classes {
		pkg.Classes = append(pkg.Classes, *c)
	}
	cov.Packages = append(cov.Packages, *pkg)
	return nil
}

type fileVisitor struct {
	fset     *token.FileSet
	name     string
	astFile  *ast.File
	coverage *Coverage
	classes  map[string]*Class
	pkg      *Package
	data     []byte
}

func (v *fileVisitor) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.FuncDecl:
		class := v.class(n)
		method := v.method(n)
		class.Methods = append(class.Methods, *method)
		for _, line := range method.Lines {
			class.Lines = append(class.Lines, line)
		}
	}
	return v
}

func (v *fileVisitor) method(n *ast.FuncDecl) *Method {
	method := &Method{Name: n.Name.Name}
	method.Lines = []Line{}
	return method
}

func (v *fileVisitor) class(n *ast.FuncDecl) *Class {
	className := v.recvName(n)
	var class *Class = v.classes[className]
	if class == nil {
		class = &Class{Name: className, Filename: v.name, Methods: []Method{}, Lines: []Line{}}
		v.classes[className] = class
	}
	return class
}

func (v *fileVisitor) recvName(n *ast.FuncDecl) string {
	if n.Recv == nil {
		return "-"
	}
	recv := n.Recv.List[0].Type
	start := v.fset.Position(recv.Pos())
	end := v.fset.Position(recv.End())
	name := string(v.data[start.Offset:end.Offset])
	return strings.TrimSpace(strings.TrimLeft(name, "*"))
}

func stripKnownSources(sources []Source, fileName string) string {
	for _, source := range sources {
		prefix := source.Path
		prefix = strings.TrimSuffix(prefix, string(os.PathSeparator)) + string(os.PathSeparator)
		if strings.HasPrefix(fileName, prefix) {
			return strings.TrimPrefix(fileName, prefix)
		}
	}
	return fileName
}
