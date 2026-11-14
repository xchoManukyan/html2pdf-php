// Command html2pdf converts HTML documents to PDF using only the Go
// standard library.
//
//	go run .                          # web UI on http://localhost:8080
//	go run . -addr :9000              # different port
//	go run . -in page.html -out page.pdf [-size Letter -landscape -margin 20 -numbers]
//	go run . -in site.zip                        # HTML + assets bundle
//	go run . -in page.html -remote -user-password secret -no-copy
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xchoManukyan/html2pdf/converter/internal/converter"
	"github.com/xchoManukyan/html2pdf/converter/internal/fonts"
	"github.com/xchoManukyan/html2pdf/converter/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "address for the web server")
	in := flag.String("in", "", "convert this HTML file and exit (CLI mode)")
	out := flag.String("out", "", "output PDF path (default: input name with .pdf)")
	size := flag.String("size", "A4", "page size: A3, A4, A5, Letter, Legal")
	landscape := flag.Bool("landscape", false, "landscape orientation")
	margin := flag.Float64("margin", 15, "page margin in millimetres")
	numbers := flag.Bool("numbers", false, "print page numbers")
	cssLib := flag.String("css", "auto", "CSS frameworks: auto (detect from the page, several possible), none, or a list of tailwind, bootstrap5, bootstrap4 (e.g. bootstrap5,tailwind)")
	viewportW := flag.Float64("viewport", 0, "layout width in CSS pixels, scaled to the paper (e.g. 1280 for a desktop layout, 375 for mobile; 0 = paper width)")
	remote := flag.Bool("remote", false, "CLI: allow downloading http(s) images, stylesheets and fonts")
	allowRemote := flag.Bool("allow-remote", false, "server: let users enable remote resources (SSRF-protected)")
	userPW := flag.String("user-password", "", "password required to open the PDF")
	ownerPW := flag.String("owner-password", "", "password for full access to the PDF")
	noPrint := flag.Bool("no-print", false, "forbid printing (encrypts the PDF)")
	noCopy := flag.Bool("no-copy", false, "forbid copying text (encrypts the PDF)")
	root := flag.String("root", "", "CLI: folder the document may load files from (default: its own folder); use it when pages reference ../css or ../img")
	noModify := flag.Bool("no-modify", false, "forbid editing and annotating (encrypts the PDF)")
	docName := flag.String("doc", "", "CLI with -in -: the document's path inside -root, for relative links (default document.html)")
	jsonOut := flag.Bool("json", false, "CLI: print a JSON report (file, size, time, CSS libraries, missing resources, or the error) for programs")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *in != "" {
		req := converter.Request{
			PageSize: *size, Landscape: *landscape, MarginMM: *margin, PageNumbers: *numbers, ViewportWidth: *viewportW, CSSLib: *cssLib,
			AllowRemote: *remote, UserPassword: *userPW, OwnerPassword: *ownerPW, DenyPrint: *noPrint, DenyCopy: *noCopy, DenyModify: *noModify,
		}
		// passwords can also come from the environment, so they don't show
		// up in the process list
		if req.UserPassword == "" {
			req.UserPassword = os.Getenv("HTML2PDF_USER_PASSWORD")
		}
		if req.OwnerPassword == "" {
			req.OwnerPassword = os.Getenv("HTML2PDF_OWNER_PASSWORD")
		}
		os.Exit(runCLI(*in, *out, *root, *docName, *jsonOut, req))
	}

	// Index installed fonts in the background so the first request is fast.
	go fonts.System()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(server.Config{AllowRemote: *allowRemote}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
	}
	go func() {
		log.Printf("HTML to PDF converter listening on http://localhost%s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("server stopped")
}
