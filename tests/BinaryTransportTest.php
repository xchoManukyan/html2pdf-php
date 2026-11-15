<?php

declare(strict_types=1);

namespace Html2Pdf\Tests;

use Html2Pdf\BinaryInstaller;
use Html2Pdf\Exception\Html2PdfException;
use Html2Pdf\Html2Pdf;
use Html2Pdf\Transport\BinaryTransport;
use Html2Pdf\Transport\TransportInterface;

final class BinaryTransportTest extends ConversionTestCase
{
    protected function transport(): TransportInterface
    {
        return new BinaryTransport();
    }

    public function testBundledBinaryMatchesThePackage(): void
    {
        self::assertFileExists((new BinaryTransport())->getBinary());
        // the binary is the converter version pinned in resources/binaries.json
        self::assertSame((new BinaryInstaller())->version(), (new BinaryTransport())->version());
    }

    public function testDefaultTransportIsTheBinary(): void
    {
        self::assertStringStartsWith('%PDF-', (new Html2Pdf())->loadHtml('<p>x</p>')->output());
    }

    public function testMissingBinary(): void
    {
        $this->expectException(Html2PdfException::class);
        new BinaryTransport(__DIR__ . '/no-such-binary');
    }

    /** @runInSeparateProcess */
    public function testStream(): void
    {
        ob_start();
        (new Html2Pdf())->loadHtml('<p>streamed</p>')->stream('Report (final).pdf', ['Attachment' => false]);
        self::assertStringStartsWith('%PDF-', (string) ob_get_clean());
    }
}
