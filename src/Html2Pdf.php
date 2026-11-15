<?php

declare(strict_types=1);

namespace Html2Pdf;

use Html2Pdf\Exception\Html2PdfException;
use Html2Pdf\Transport\BinaryTransport;
use Html2Pdf\Transport\TransportInterface;

/**
 * HTML to PDF, with the same workflow as dompdf:
 *
 *     $pdf = new Html2Pdf();
 *     $pdf->loadHtml($html);
 *     $pdf->setPaper('A4', 'landscape');
 *     $pdf->render();
 *     $pdf->stream('invoice.pdf');
 *
 * or in one call: Html2Pdf::convert($html, ['paperSize' => 'Letter']).
 */
final class Html2Pdf
{
    public const VERSION = '0.1.2';

    /** @var Options */
    private $options;
    /** @var TransportInterface|null created on first use */
    private $transport;
    /** @var string|null */
    private $html;
    /** @var string */
    private $documentName = 'document.html';
    /** @var Result|null */
    private $result;

    /**
     * @param Options|array<string, mixed>|null $options
     * @param TransportInterface|null           $transport default: the bundled binary
     */
    public function __construct($options = null, ?TransportInterface $transport = null)
    {
        $this->setOptions($options ?? new Options());
        $this->transport = $transport;
    }

    /**
     * Converts in one call and returns the PDF contents.
     *
     * @param Options|array<string, mixed>|null $options
     */
    public static function convert(string $html, $options = null, ?TransportInterface $transport = null): string
    {
        return (new self($options, $transport))->loadHtml($html)->render()->getPdf();
    }

    /** @param Options|array<string, mixed> $options */
    public function setOptions($options): self
    {
        if (is_array($options)) {
            $options = new Options($options);
        }
        if (!$options instanceof Options) {
            throw new Html2PdfException('Options must be an Html2Pdf\Options object or an array');
        }
        $this->options = $options;
        $this->result = null;
        return $this;
    }

    public function getOptions(): Options
    {
        return $this->options;
    }

    public function setTransport(TransportInterface $transport): self
    {
        $this->transport = $transport;
        return $this;
    }

    /**
     * Loads the document. $encoding converts it to UTF-8 first (needs
     * mbstring); without it, a <meta charset> in the document is honoured.
     */
    public function loadHtml(string $html, ?string $encoding = null): self
    {
        if ($encoding !== null && !in_array(strtoupper($encoding), ['UTF-8', 'UTF8'], true)) {
            if (!function_exists('mb_convert_encoding')) {
                throw new Html2PdfException('Converting from ' . $encoding . ' needs the mbstring extension');
            }
            $html = (string) mb_convert_encoding($html, 'UTF-8', $encoding);
        }
        $this->html = $html;
        $this->result = null;
        return $this;
    }

    /**
     * Loads an HTML file. Its folder becomes the base path (unless one is
     * set and contains the file), so its stylesheets and images load.
     */
    public function loadHtmlFile(string $file): self
    {
        $real = realpath($file);
        $html = $real === false ? false : @file_get_contents($real);
        if ($real === false || $html === false) {
            throw new Html2PdfException("Cannot read \"$file\"");
        }
        $real = str_replace('\\', '/', $real);
        $base = $this->options->getBasePath();
        $root = $base === null ? '' : rtrim(str_replace('\\', '/', $base), '/') . '/';
        if ($base === null || strpos($real, $root) !== 0) {
            $this->options->setBasePath(dirname($real));
            $root = rtrim(str_replace('\\', '/', (string) $this->options->getBasePath()), '/') . '/';
        }
        $this->html = $html;
        $this->setDocumentName(substr($real, strlen($root)));
        $this->result = null;
        return $this;
    }

    /**
     * The document's path inside the base path ("reports/q3.html"), so its
     * relative links resolve as in the browser. Also names the PDF in
     * stream() when no file name is given.
     */
    public function setDocumentName(string $name): self
    {
        $name = trim(str_replace('\\', '/', $name), '/');
        if (!preg_match('/\.(html?|xhtml)$/i', $name)) {
            $name .= '.html';
        }
        $this->documentName = $name;
        return $this;
    }

    /**
     * Paper size and orientation, as in dompdf: 'A4', 'letter', ... or a
     * custom size, either [width, height] in millimetres or dompdf's
     * [0, 0, width, height] in points.
     *
     * @param string|array<int, float|int> $size
     */
    public function setPaper($size, string $orientation = Options::PORTRAIT): self
    {
        if (is_array($size) && count($size) === 4) {
            $toMm = static function ($pt): float {
                return round((float) $pt * 25.4 / 72, 2);
            };
            $size = [$toMm($size[2] - $size[0]), $toMm($size[3] - $size[1])];
        }
        $this->options->setPaperSize($size)->setOrientation($orientation);
        $this->result = null;
        return $this;
    }

    /** Folder the document may load local files from (dompdf: chroot). */
    public function setBasePath(string $path): self
    {
        $this->options->setBasePath($path);
        $this->result = null;
        return $this;
    }

    /** Converts the loaded document. */
    public function render(): Result
    {
        if ($this->html === null) {
            throw new Html2PdfException('Load a document first (loadHtml or loadHtmlFile)');
        }
        if ($this->transport === null) {
            $this->transport = new BinaryTransport();
        }
        return $this->result = $this->transport->convert($this->html, $this->documentName, $this->options);
    }

    /** The result of the last render(), with warnings about missing resources. */
    public function getResult(): ?Result
    {
        return $this->result;
    }

    /** The PDF contents (renders first if needed). */
    public function output(): string
    {
        return ($this->result ?? $this->render())->getPdf();
    }

    /** Writes the PDF to a file (renders first if needed). */
    public function save(string $path): void
    {
        ($this->result ?? $this->render())->save($path);
    }

    /**
     * Sends the PDF to the browser, as dompdf does: a download by default,
     * shown in the browser with ['Attachment' => false].
     *
     * @param array{Attachment?: bool|int} $options
     */
    public function stream(?string $filename = null, array $options = []): void
    {
        $pdf = $this->output();
        $filename = $filename ?? preg_replace('/\.[^.\/]*$/', '', basename($this->documentName)) . '.pdf';
        if (!preg_match('/\.pdf$/i', $filename)) {
            $filename .= '.pdf';
        }
        $attachment = !isset($options['Attachment']) || $options['Attachment'];
        $ascii = preg_replace('/[^\x20-\x7e]|["\\\\]/', '_', $filename);
        if (headers_sent($file, $line)) {
            throw new Html2PdfException("Cannot stream the PDF: output already started in $file:$line");
        }
        header('Content-Type: application/pdf');
        header('Content-Length: ' . strlen($pdf));
        header(sprintf('Content-Disposition: %s; filename="%s"; filename*=UTF-8\'\'%s',
            $attachment ? 'attachment' : 'inline', $ascii, rawurlencode($filename)));
        header('Cache-Control: private, max-age=0, must-revalidate');
        echo $pdf;
    }
}
