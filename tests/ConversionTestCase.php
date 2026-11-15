<?php

declare(strict_types=1);

namespace Html2Pdf\Tests;

use Html2Pdf\Exception\ConversionException;
use Html2Pdf\Html2Pdf;
use Html2Pdf\Options;
use Html2Pdf\Transport\TransportInterface;
use PHPUnit\Framework\TestCase;

/**
 * The same conversions through each transport (binary, HTTP server).
 */
abstract class ConversionTestCase extends TestCase
{
    abstract protected function transport(): TransportInterface;

    /** Options that sign with the test certificate. */
    protected function signing(): Options
    {
        $dir = __DIR__ . '/fixtures/signing/';
        return (new Options())->setSignature($dir . 'test-cert.pem', $dir . 'test-key.pem', ['reason' => 'Approved']);
    }

    /** @param Options|array<string, mixed>|null $options */
    private function pdf($options = null): Html2Pdf
    {
        return new Html2Pdf($options, $this->transport());
    }

    private static function site(string $path = ''): string
    {
        return __DIR__ . '/fixtures/site' . ($path === '' ? '' : '/' . $path);
    }

    public function testConvertsHtml(): void
    {
        $result = $this->pdf()->loadHtml('<h1>Hello</h1><p>Wörld €</p>')->render();
        self::assertStringStartsWith('%PDF-', $result->getPdf());
        self::assertStringContainsString('%%EOF', $result->getPdf());
        self::assertSame([], $result->getFailures());
        self::assertGreaterThanOrEqual(0, $result->getRenderTimeMs());
    }

    public function testDetectsCssLibraries(): void
    {
        $html = '<div class="row"><div class="col-md-6"><button class="btn btn-primary">Pay</button></div>'
            . '<div class="col-md-6 d-flex justify-content-end">x</div></div>';
        $result = $this->pdf()->loadHtml($html)->render();
        self::assertSame([['lib' => 'bootstrap5', 'source' => 'classes']], $result->getCssLibraries());
    }

    public function testChosenCssLibraries(): void
    {
        $result = $this->pdf(['cssLibraries' => ['tailwind', 'bootstrap5']])->loadHtml('<p class="p-4">x</p>')->render();
        self::assertSame([
            ['lib' => 'bootstrap5', 'source' => 'selected'],
            ['lib' => 'tailwind', 'source' => 'selected'],
        ], $result->getCssLibraries());
        $result = $this->pdf(['cssLibraries' => 'none'])->loadHtml('<button class="btn btn-primary">x</button>')->render();
        self::assertSame([], $result->getCssLibraries());
    }

    public function testLoadsLocalFilesAndReportsMissingOnes(): void
    {
        $pdf = $this->pdf();
        $result = $pdf->loadHtmlFile(self::site('index.html'))->render();
        self::assertSame(['img/missing.png'], $result->getMissingResources(), 'the stylesheet and the SVG load, one image is missing');
        self::assertSame('resource not found', $result->getFailures()[0]['reason']);
        self::assertTrue($result->hasMissingResources());
        $content = self::content($result->getPdf());
        if ($content !== '') {
            self::assertStringContainsString('1 0 0 rg', $content, 'red title from css/style.css');
        }
    }

    public function testDocumentInASubfolder(): void
    {
        $pdf = $this->pdf()->setBasePath(self::site());
        $result = $pdf->loadHtmlFile(self::site('pages/sub.html'))->render();
        self::assertSame([], $result->getMissingResources(), '../css/style.css and the SVG it uses resolve inside the base path');
    }

    public function testLoadHtmlWithBasePath(): void
    {
        $html = (string) file_get_contents(self::site('index.html'));
        $result = $this->pdf(['basePath' => self::site()])->loadHtml($html)->render();
        self::assertSame(['img/missing.png'], $result->getMissingResources());
        // without a base path no local file can be read
        $result = $this->pdf()->loadHtml($html)->render();
        self::assertSame(['css/style.css', 'img/logo.svg', 'img/missing.png'], self::sorted($result->getMissingResources()));
    }

    public function testDompdfPaperInPoints(): void
    {
        $pdf = $this->pdf()->loadHtml('<p>receipt</p>')->setPaper([0, 0, 226.77, 566.93]);
        self::assertStringContainsString('/MediaBox [0 0 226.772 566.929]', $pdf->output());
        $pdf = $this->pdf()->loadHtml('<p>x</p>')->setPaper('letter', 'landscape');
        self::assertStringContainsString('/MediaBox [0 0 792 612]', $pdf->output());
    }

    public function testPasswordProtection(): void
    {
        $pdf = $this->pdf((new Options())->setPassword('secret'))->loadHtml('<p>private</p>')->output();
        self::assertStringContainsString('/Encrypt', $pdf);
        $pdf = $this->pdf((new Options())->setPermissions(false))->loadHtml('<p>no printing</p>')->output();
        self::assertStringContainsString('/Encrypt', $pdf);
    }

    public function testStrongEncryptionByDefault(): void
    {
        $pdf = $this->pdf((new Options())->setPassword('secret'))->loadHtml('<p>x</p>')->output();
        self::assertStringContainsString('/V 5 /R 6', $pdf, 'AES-256');
        $pdf = $this->pdf((new Options())->setPassword('secret')->setEncryption(Options::AES128))->loadHtml('<p>x</p>')->output();
        self::assertStringContainsString('/V 4 /R 4', $pdf, 'AES-128');
    }

    public function testPdfA(): void
    {
        try {
            $pdf = $this->pdf((new Options())->setPdfA())->loadHtml('<p style="font-family: Arial, sans-serif">Archive</p>')->output();
        } catch (ConversionException $e) {
            self::markTestSkipped('no installed fonts to embed: ' . $e->getMessage());
        }
        self::assertStringContainsString('<pdfaid:part>2</pdfaid:part>', $pdf);
        self::assertStringContainsString('/GTS_PDFA1', $pdf);
    }

    public function testFillableForms(): void
    {
        $html = '<form><input name="customer" value="Ada"><input type="checkbox" name="ok" checked>'
            . '<select name="plan"><option>A</option><option selected>B</option></select><textarea name="notes">x</textarea></form>';
        $pdf = $this->pdf((new Options())->setFillableForms())->loadHtml($html)->output();
        self::assertStringContainsString('/AcroForm', $pdf);
        self::assertStringContainsString('/FT /Tx /T (customer)', $pdf);
        self::assertStringContainsString('/FT /Ch /T (plan)', $pdf);
        self::assertSame(4, substr_count($pdf, '/Subtype /Widget'));
        self::assertStringNotContainsString('/AcroForm', $this->pdf()->loadHtml($html)->output());
    }

    public function testSignature(): void
    {
        $pdf = $this->pdf($this->signing())->loadHtml('<p>Contract</p>')->output();
        self::assertStringContainsString('/SubFilter /ETSI.CAdES.detached', $pdf);
        self::assertStringContainsString('/Reason (Approved)', $pdf);
        if (preg_match('~/ByteRange \[0 (\d+) (\d+) (\d+)\s*\]~', $pdf, $m) !== 1) {
            self::fail('no signature byte range');
        }
        [$contents, $after, $rest] = [(int) $m[1], (int) $m[2], (int) $m[3]];
        self::assertSame(strlen($pdf), $after + $rest, 'the signed ranges cover the whole file');
        self::assertSame('<', $pdf[$contents]);
    }

    public function testBarcodeElement(): void
    {
        $result = $this->pdf()->loadHtml('<p>Ticket</p><barcode code="ABC-123" type="C128" text="1"><barcode code="https://example.com" type="QR">')->render();
        self::assertStringStartsWith('%PDF-', $result->getPdf());
        self::assertSame([], $result->getFailures());
    }

    public function testRejectsEmptyDocument(): void
    {
        $this->expectException(ConversionException::class);
        $this->expectExceptionMessage('empty');
        $this->pdf()->loadHtml('   ')->render();
    }

    public function testSave(): void
    {
        $file = tempnam(sys_get_temp_dir(), 'h2pt');
        try {
            $this->pdf()->loadHtml('<p>saved</p>')->save($file);
            self::assertStringStartsWith('%PDF-', (string) file_get_contents($file));
        } finally {
            @unlink($file);
        }
    }

    public function testOneCall(): void
    {
        self::assertStringStartsWith('%PDF-', Html2Pdf::convert('<p>x</p>', ['paperSize' => 'A5'], $this->transport()));
    }

    /**
     * @param string[] $list
     *
     * @return string[]
     */
    private static function sorted(array $list): array
    {
        sort($list);
        return $list;
    }

    /** The page content streams, uncompressed (empty when zlib is missing). */
    private static function content(string $pdf): string
    {
        if (!function_exists('gzuncompress')) {
            return '';
        }
        $out = '';
        if (preg_match_all('/stream\r?\n(.*?)\r?\nendstream/s', $pdf, $m)) {
            foreach ($m[1] as $stream) {
                $data = @gzuncompress($stream);
                if ($data !== false) {
                    $out .= $data;
                }
            }
        }
        return $out;
    }
}
