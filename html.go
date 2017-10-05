/*
	Most of this file was taken from https://github.com/golang/tools/blob/f1a397bba50dee815e8c73f3ec94ffc0e8df1a09/cmd/cover/html.go
	which has all the functions `go tool` uses to generate the HTML we want, none of which are exported

	I've left the copyright notice here, but have modified functions, function names, arguments, etc.
*/

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/build"
	"html/template"
	"io"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/tools/cover"
)

const (
	tarpClassName = "tarp-uncovered"
	tarpColor     = "rgb(252, 242, 106)"
	tmplHTML      = `
<!DOCTYPE html>
<html>
	<head>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
		<style>
			body {
				background: black;
				color: rgb(80, 80, 80);
			}
			body, pre, #legend span {
				font-family: Menlo, monospace;
				font-weight: bold;
			}
			#topbar {
				background: black;
				position: fixed;
				top: 0; left: 0; right: 0;
				height: 42px;
				border-bottom: 1px solid rgb(80, 80, 80);
			}
			#content {
				margin-top: 50px;
			}
			#nav, #legend {
				float: left;
				margin-left: 10px;
			}
			#legend {
				margin-top: 12px;
			}
			#nav {
				margin-top: 10px;
			}
			#legend span {
				margin: 0 1px;
			}
			{{colors}}
		</style>
	</head>
	<body>
		<div id="topbar">
			<div id="nav">
				<select id="files">
{{range $i, $f := .Files}}
				<option value="file{{$i}}">{{$f.Name}} ({{printf "%.1f" $f.Coverage}}%)</option>
{{end}}
				</select>
			</div>
			<div id="legend">
				<span>not tracked</span>
{{if .Set}}
				<span class="cov0">not covered</span>
				<span class="cov8">covered</span>
{{else}}
				<span class="cov0">no coverage</span>
				<span class="cov1">low coverage</span>
				<span class="cov2">*</span>
				<span class="cov3">*</span>
				<span class="cov4">*</span>
				<span class="cov5">*</span>
				<span class="cov6">*</span>
				<span class="cov7">*</span>
				<span class="cov8">*</span>
				<span class="cov9">*</span>
				<span class="cov10">high coverage</span>
{{end}}
				<span class="tarp-uncovered">indirectly covered</span>
			</div>
		</div>
		<div id="content">
{{range $i, $f := .Files}}
		<pre class="file" id="file{{$i}}" {{if $i}}style="display: none"{{end}}>{{$f.Body}}</pre>
{{end}}
		</div>
	</body>
	<script>
	(function() {
		let files = document.getElementById('files');
		let visible = document.getElementById('file0');
		files.addEventListener('change', onChange, false);
		function onChange() {
			visible.style.display = 'none';
			visible = document.getElementById(files.value);
			visible.style.display = 'block';
			window.scrollTo(0, 0);
		}
	})();
	</script>
</html>
`
)

var htmlTemplate = template.Must(template.New("html").Funcs(template.FuncMap{"colors": CSScolors}).Parse(tmplHTML))

type templateData struct {
	Files []*templateFile
	Set   bool
}

type templateFile struct {
	Name     string
	Body     template.HTML
	Coverage float64
}

// findFile finds the location of the named file in GOROOT, GOPATH etc.
func findFile(path string) (string, error) {
	dir, file := filepath.Split(path)
	pkg, err := build.Import(dir, ".", build.FindOnly)
	if err != nil {
		return "", fmt.Errorf("can't find %q: %v", file, err)
	}
	return filepath.Join(pkg.Dir, file), nil
}

// htmlOutput reads the profile data from profile and generates an HTML
// coverage report, writing it to outfile. If outfile is empty,
// it writes the report to a temporary file and opens it in a web browser.
func htmlOutput(profilePath, outfile string, report tarpReport) error {
	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil {
		return err
	}

	var d templateData

	for _, profile := range profiles {
		fn := profile.FileName
		if profile.Mode == "set" {
			d.Set = true
		}
		file, err := findFile(fn)
		if err != nil {
			return err
		}
		src, err := ioutil.ReadFile(file)
		if err != nil {
			return fmt.Errorf("can't read %q: %v", fn, err)
		}

		boundaries := profile.Boundaries(src)
		var buf bytes.Buffer
		err = htmlGen(&buf, src, fn, boundaries, report)
		if err != nil {
			return err
		}
		bufString := buf.String()
		d.Files = append(d.Files, &templateFile{
			Name:     fn,
			Body:     template.HTML(bufString),
			Coverage: percentCovered(profile),
		})
	}

	var out *os.File
	if outfile == "" {
		var dir string
		dir, err = ioutil.TempDir("", "cover")
		if err != nil {
			return err
		}
		out, err = os.Create(filepath.Join(dir, "coverage.html"))
	} else {
		out, err = os.Create(outfile)
	}
	if err != nil {
		return err
	}
	err = htmlTemplate.Execute(out, d)
	if err == nil {
		err = out.Close()
	}
	if err != nil {
		return err
	}

	if outfile == "" {
		if !startBrowser(fmt.Sprintf("file://%s", out.Name()), goose()) {
			fmt.Fprintf(os.Stderr, "HTML output written to %s\n", out.Name())
		}
	}

	return nil
}

// percentCovered returns, as a percentage, the fraction of the statements in
// the profile covered by the test run.
// In effect, it reports the coverage of a given source file.
func percentCovered(p *cover.Profile) float64 {
	var total, covered int64
	for _, b := range p.Blocks {
		total += int64(b.NumStmt)
		if b.Count > 0 {
			covered += int64(b.NumStmt)
		}
	}
	if total == 0 {
		return 0
	}
	return float64(covered) / float64(total) * 100
}

// htmlGen generates an HTML coverage report with the provided filename,
// source code, and tokens, and writes it to the given Writer.
func htmlGen(w io.Writer, src []byte, filename string, boundaries []cover.Boundary, report tarpReport) error {
	dst := bufio.NewWriter(w)

	currentLine := 1
	for i := range src {
		for len(boundaries) > 0 && boundaries[0].Offset == i {
			b := boundaries[0]
			if b.Start {
				var relevantFunc tarpFunc
				for _, d := range report.DeclaredDetails {
					if strings.Contains(d.Filename, filename) {
						if d.RBracePos.Line == currentLine {
							relevantFunc = d
							break
						}
					}
				}
				relevantFuncCalled := report.Called.Has(relevantFunc.Name)

				n := 0
				if b.Count > 0 {
					n = int(math.Floor(b.Norm*9)) + 1
				}
				if relevantFunc.Name != "" && n > 0 && !relevantFuncCalled {
					fmt.Fprintf(dst, `<span class="%s" title="%v">`, tarpClassName, b.Count)
				} else {
					fmt.Fprintf(dst, `<span class="cov%v" title="%v">`, n, b.Count)
				}
			} else {
				dst.WriteString("</span>")
			}
			boundaries = boundaries[1:]
		}
		switch b := src[i]; b {
		case '>':
			dst.WriteString("&gt;")
		case '<':
			dst.WriteString("&lt;")
		case '&':
			dst.WriteString("&amp;")
		case '\t':
			dst.WriteString("    ")
		case '\n':
			currentLine++
			dst.WriteByte(b)
		default:
			dst.WriteByte(b)
		}
	}
	return dst.Flush()
}

// goose is a wrapper for runtime.GOOS that we can actually monkey patch
func goose() string {
	return runtime.GOOS
}

// startBrowser tries to open the URL in a browser
// and reports whether it succeeds.
func startBrowser(url string, os string) bool {
	// try to start the browser
	var args []string
	switch os {
	case "darwin":
		args = []string{"open"}
	case "windows":
		args = []string{"cmd", "/c", "start"}
	default:
		args = []string{"xdg-open"}
	}
	command := exec.Command(args[0], append(args[1:], url)...)
	return command.Start() == nil
}

// rgb returns an rgb value for the specified coverage value
// between 0 (no coverage) and 10 (max coverage).
func rgb(n int) string {
	if n == 0 {
		return "rgb(192, 0, 0)" // Red
	}
	// Gradient from gray to green.
	r := 128 - 12*(n-1)
	g := 128 + 12*(n-1)
	b := 128 + 3*(n-1)
	return fmt.Sprintf("rgb(%v, %v, %v)", r, g, b)
}

// colors generates the CSS rules for coverage colors.
func CSScolors() template.CSS {
	var buf bytes.Buffer
	for i := 0; i < 11; i++ {
		fmt.Fprintf(&buf, ".cov%v { color: %v }\n\t\t\t", i, rgb(i))
	}
	fmt.Fprint(&buf, fmt.Sprintf(".%s { color: %s }\n", tarpClassName, tarpColor))
	return template.CSS(buf.String())
}