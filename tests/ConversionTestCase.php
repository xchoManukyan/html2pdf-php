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
