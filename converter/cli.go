package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xchoManukyan/html2pdf/converter/internal/converter"
)

// version is reported by -version (and used by the PHP package to check
// that its binary matches).
const version = "0.1.0"

// report is the -json output.
type report struct {
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	File     string    `json:"file,omitempty"`
	Bytes    int       `json:"bytes,omitempty"`
	TookMs   int64     `json:"tookMs"`
	CSSLibs  []cssLib  `json:"cssLibs"`
	Missing  []string  `json:"missing"`
	Failures []failure `json:"failures"`
	Version  string    `json:"version"`
}

type cssLib struct {
	Lib    string `json:"lib"`
	Source string `json:"source"`
}

type failure struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

// runCLI converts one document and returns the exit code. in may be "-"
// for standard input, out "-" for standard output (the report then goes
// to standard error).
func runCLI(in, out, root, doc string, asJSON bool, req converter.Request) int {
	rep := report{Version: version, CSSLibs: []cssLib{}, Missing: []string{}, Failures: []failure{}}
	repOut := io.Writer(os.Stdout)
	if out == "-" {
		repOut = os.Stderr
	}
	err := convert(in, out, root, doc, req, &rep)
	if asJSON {
		if err != nil {
			rep.Error = err.Error()
		}
		rep.OK = err == nil
		enc := json.NewEncoder(repOut)
		enc.Encode(rep)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	} else {
		fmt.Fprintf(repOut, "wrote %s (%d bytes, %dms)\n", rep.File, rep.Bytes, rep.TookMs)
		for _, d := range rep.CSSLibs {
			fmt.Fprintf(repOut, "  CSS library: %s (%s)\n", d.Lib, d.Source)
		}
		for _, f := range rep.Failures {
			fmt.Fprintf(repOut, "  not loaded: %s (%s)\n", f.Ref, f.Reason)
		}
	}
	if err != nil {
		return 1
	}
	return 0
}

func convert(in, out, root, doc string, req converter.Request, rep *report) error {
	var data []byte
	var err error
	if in == "-" {
		// the document comes from a program: local files only below -root
		if data, err = io.ReadAll(io.LimitReader(os.Stdin, converter.MaxBundleSize+1)); err != nil {
			return err
		}
		doc = strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+orDefault(doc, "document.html"))), "/")
		req.FileName = filepath.Base(doc)
		if root != "" {
			if req.BaseDir, err = filepath.Abs(root); err != nil {
				return err
			}
			req.DocPath = doc
		}
	} else {
		if data, err = os.ReadFile(in); err != nil {
			return err
		}
		req.FileName = filepath.Base(in)
		req.BaseDir = filepath.Dir(in) // relative images, stylesheets and fonts load from here
		if root != "" {
			absIn, err1 := filepath.Abs(in)
			absRoot, err2 := filepath.Abs(root)
			rel, err3 := filepath.Rel(absRoot, absIn)
			if err1 != nil || err2 != nil || err3 != nil || strings.HasPrefix(rel, "..") {
				return fmt.Errorf("%s is not inside -root %s", in, root)
			}
			req.BaseDir, req.DocPath = absRoot, filepath.ToSlash(rel)
		}
	}
	req.HTML = data
	res, err := converter.Convert(req)
	if err != nil {
		return err
	}
	switch {
	case out == "-":
		if _, err := os.Stdout.Write(res.PDF); err != nil {
			return err
		}
	default:
		if out == "" {
			dir := "."
			if in != "-" {
				dir = filepath.Dir(in)
			}
			out = filepath.Join(dir, res.FileName)
		}
		if err := os.WriteFile(out, res.PDF, 0o644); err != nil {
			return err
		}
	}
	rep.File, rep.Bytes, rep.TookMs = out, len(res.PDF), res.Took.Round(time.Millisecond).Milliseconds()
	for _, d := range res.CSSLibs {
		rep.CSSLibs = append(rep.CSSLibs, cssLib{d.Lib, d.Source})
	}
	rep.Missing = append(rep.Missing, res.Missing...)
	for _, f := range res.Failures {
		rep.Failures = append(rep.Failures, failure{f.Ref, f.Reason})
	}
	return nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
