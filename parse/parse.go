package parse

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
	"unicode"

	"golang.org/x/tools/imports"
)

type isExported bool

var header = []byte(`

// This file was automatically generated by genny.
// Any changes will be lost if this file is regenerated.
// see https://github.com/cheekybits/genny

`)

var ctypes = map[string]string{
	"float64": "C.double",
	"float32": "C.float",
	"int":     "C.int",
	"uint":    "C.uint",
	"int32":   "C.int",
	"uint32":  "C.uint",
	"int64":   "C.long",
	"uint64":  "C.ulong",
}

var (
	packageKeyword = []byte("package")
	importKeyword  = []byte("import")
	openBrace      = []byte("(")
	closeBrace     = []byte(")")
	space          = " "
	genericPackage = "generic"
	genericType    = "generic.Type"
	genericNumber  = "generic.Number"
	genericCType   = "generic.CType"
	genericCNumber = "generic.CNumber"
	linefeed       = "\r\n"
)
var unwantedLinePrefixes = [][]byte{
	[]byte("//go:generate genny "),
}

func generateSpecific(filename string, in io.ReadSeeker, typeSet map[string]string) ([]byte, bool, error) {
	usedC := false
	// ensure we are at the beginning of the file
	in.Seek(0, os.SEEK_SET)

	// parse the source file
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, filename, in, 0)
	if err != nil {
		return nil, false, &errSource{Err: err}
	}

	// make sure every generic.Type is represented in the types
	// argument.
	for _, decl := range file.Decls {
		switch it := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range it.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				switch tt := ts.Type.(type) {
				case *ast.SelectorExpr:
					if name, ok := tt.X.(*ast.Ident); ok {
						if name.Name == genericPackage {
							if _, ok := typeSet[ts.Name.Name]; !ok {
								if ts.Name.Name[0] == 'C' {
									if _, ok = typeSet[ts.Name.Name[1:]]; !ok {
										return nil, false, &errMissingSpecificType{GenericType: ts.Name.Name}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// go back to the start of the file
	in.Seek(0, os.SEEK_SET)

	var buf bytes.Buffer

	comment := ""
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {

		l := scanner.Text()

		// does this line contain generic.Type?
		if strings.Contains(l, genericType) || strings.Contains(l, genericNumber) ||
			strings.Contains(l, genericCType) || strings.Contains(l, genericCNumber) {
			comment = ""
			continue
		}

		for t, specificType := range typeSet {

			// does the line contain our type
			if strings.Contains(l, t) {

				var newLine string
				// check each word
				for _, word := range strings.Fields(l) {

					i := 0
					for {
						i = strings.Index(word[i:], t) // find out where

						if i > -1 {

							// if this isn't an exact match
							if i > 0 && isAlphaNumeric(rune(word[i-1])) || i < len(word)-len(t) && isAlphaNumeric(rune(word[i+len(t)])) {
								// replace the word with a capitolized version
								if UseCType(word, t, i) {
									word = strings.Replace(word, "C"+t, ctypes[specificType], 1)
									usedC = true
								} else {
									periodIdx := strings.Index(word, ".")
									exported := unicode.IsUpper(rune(strings.TrimLeft(word[periodIdx+1:], "*&(")[0]))
									word = strings.Replace(word, t, wordify(specificType, exported), 1)
								}
							} else {
								// replace the word as is
								word = strings.Replace(word, t, specificType, 1)
							}

						} else {
							newLine = newLine + word + space
							break
						}

					}
				}
				l = newLine
			}
		}

		if comment != "" {
			buf.WriteString(line(comment))
			comment = ""
		}

		// is this line a comment?
		// TODO: should we handle /* */ comments?
		if strings.HasPrefix(l, "//") {
			// record this line to print later
			comment = l
			continue
		}

		// write the line
		buf.WriteString(line(l))
	}

	// write it out
	return buf.Bytes(), usedC, nil
}

func UseCType(word, t string, i int) bool {
	if i > 0 && word[i-1] == 'C' && (len(word) == (len(t)+i) || !isAlphaNumeric(rune(word[i+len(t)]))) {
		return (i == 1) || !isAlphaNumeric(rune(word[i-2]))
	}
	return false
}

// Generics parses the source file and generates the bytes replacing the
// generic types for the keys map with the specific types (its value).
func Generics(filename, pkgName string, in io.ReadSeeker, typeSets []map[string]string) ([]byte, error) {

	totalOutput := header
	needC := false
	for _, typeSet := range typeSets {

		// generate the specifics
		parsed, usedC, err := generateSpecific(filename, in, typeSet)
		if err != nil {
			return nil, err
		}

		needC = needC || usedC
		totalOutput = append(totalOutput, parsed...)

	}

	// clean up the code line by line
	packageFound := false
	insideImportBlock := false
	packageNumber := 0
	var cleanOutputLines []string
	scanner := bufio.NewScanner(bytes.NewReader(totalOutput))
	for scanner.Scan() {

		// end of imports block?
		if insideImportBlock {
			if bytes.HasSuffix(scanner.Bytes(), closeBrace) {
				insideImportBlock = false
			}
			if packageNumber > 1 {
				continue
			}
		}

		if bytes.HasPrefix(scanner.Bytes(), packageKeyword) {
			packageNumber++
			if packageFound {
				continue
			} else {
				packageFound = true
			}
		} else if bytes.HasPrefix(scanner.Bytes(), importKeyword) {
			if bytes.HasSuffix(scanner.Bytes(), openBrace) {
				insideImportBlock = true
			}
			if packageNumber > 1 {
				continue
			}
		}

		// check all unwantedLinePrefixes - and skip them
		skipline := false
		for _, prefix := range unwantedLinePrefixes {
			if bytes.HasPrefix(scanner.Bytes(), prefix) {
				skipline = true
				continue
			}
		}

		if skipline {
			continue
		}

		cleanOutputLines = append(cleanOutputLines, line(scanner.Text()))
		if packageFound && needC {
			cleanOutputLines = append(cleanOutputLines, "import \"C\"\n")
			needC = false
		}

	}

	cleanOutput := strings.Join(cleanOutputLines, "")

	output := []byte(cleanOutput)

	// change package name
	if pkgName != "" {
		output = changePackage(bytes.NewReader([]byte(output)), pkgName)
	}
	// fix the imports
	var err error
	output, err = imports.Process(filename, output, nil)

	if err != nil {
		return nil, &errImports{Err: err}
	}
	return output, nil
}

func line(s string) string {
	return fmt.Sprintln(strings.TrimRight(s, linefeed))
}

// isAlphaNumeric gets whether the rune is alphanumeric or _.
func isAlphaNumeric(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// wordify turns a type into a nice word for function and type
// names etc.
func wordify(s string, exported bool) string {
	s = strings.TrimRight(s, "{}")
	s = strings.TrimLeft(s, "*&")
	s = strings.Replace(s, ".", "", -1)
	if !exported {
		return s
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}

func changePackage(r io.Reader, pkgName string) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(r)
	done := false

	for sc.Scan() {
		s := sc.Text()

		if !done && strings.HasPrefix(s, "package") {
			parts := strings.Split(s, " ")
			parts[1] = pkgName
			s = strings.Join(parts, " ")
			done = true
		}

		fmt.Fprintln(&out, s)
	}
	return out.Bytes()
}
