# Security policy

## Supported versions

Security fixes go into the latest release. Please upgrade before reporting an issue.

## Reporting a vulnerability

Please **do not open a public issue** for security problems. Problems in the converter itself (reading files,
remote requests, the server) can also be reported to
[html2pdf-core](https://github.com/xchoManukyan/html2pdf-core/security/advisories/new). Report them privately through GitHub:
[Report a vulnerability](https://github.com/xchoManukyan/html2pdf-php/security/advisories/new).

Include what an attacker can do, the affected version, and an HTML document or steps that show it. You will get an
answer within a week; fixes are released as soon as they are ready and credited to you unless you prefer otherwise.

## What the package protects against

These are deliberate guarantees, so a way around any of them is a vulnerability:

- **Local files:** a document can only read files inside the base path (`setBasePath`, dompdf's `chroot`). With no
  base path it can read none.
- **Remote resources** are off by default. When enabled, requests to private, loopback and link-local addresses are
  refused (checked after DNS resolution), and size, time and redirects are limited.
- **Converter binaries** are only used if their SHA-256 checksum matches the one shipped with the package. Release
  binaries of html2pdf-core are reproducible builds, and this package's release workflow checks that the pinned
  checksums are exactly the ones published with that release.
- **Passwords** for PDF encryption are passed to the converter in its environment, not on the command line.

## Things to keep in mind

- The converter server (`html2pdf -addr ...`, used by `HttpTransport`) has no authentication. Run it on an internal
  network or behind a proxy that authenticates.
- Converting untrusted HTML costs CPU and memory. Use `setTimeout()` and limit the size of what users can submit.
- PDF password protection uses AES-256 by default. AES-128 and RC4 (`setEncryption`) exist only for very old readers
  and are weaker.
