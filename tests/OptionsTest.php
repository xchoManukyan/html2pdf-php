<?php

declare(strict_types=1);

namespace Html2Pdf\Tests;

use Html2Pdf\Exception\Html2PdfException;
use Html2Pdf\Options;
use PHPUnit\Framework\TestCase;

final class OptionsTest extends TestCase
{
    public function testDefaults(): void
    {
        $o = new Options();
        self::assertSame(['-size', 'A4', '-margin', '15', '-css', 'auto'], $o->toArguments());
        self::assertSame([], $o->passwordEnv());
    }

    public function testArguments(): void
    {
        $o = (new Options())
            ->setPaperSize('letter')->setOrientation('LANDSCAPE')->setMarginMm(12.5)
            ->setPageNumbers()->setViewportWidth(1280)->setCssLibraries(['tailwind', 'bootstrap'])
            ->setRemoteEnabled()->setPermissions(false, false, false);
        self::assertSame([
            '-size', 'LETTER', '-margin', '12.5', '-css', 'tailwind,bootstrap5',
            '-landscape', '-numbers', '-viewport', '1280', '-remote', '-no-print', '-no-copy', '-no-modify',
        ], $o->toArguments());
    }

    public function testCustomPaperSize(): void
    {
        self::assertSame('80X200', (new Options())->setPaperSize([80, 200])->getPaperSize());
        self::assertSame('210X297.5', (new Options())->setPaperSize('210x297.5')->getPaperSize());
        $this->expectException(Html2PdfException::class);
        (new Options())->setPaperSize('B9');
    }

    public function testDompdfNames(): void
    {
        $o = new Options(['isRemoteEnabled' => true, 'defaultPaperSize' => 'a5', 'defaultPaperOrientation' => 'landscape',
            'chroot' => __DIR__ . '/fixtures/site']);
        self::assertTrue($o->isRemoteEnabled());
        self::assertSame('A5', $o->getPaperSize());
        self::assertSame('landscape', $o->getOrientation());
        self::assertSame(realpath(__DIR__ . '/fixtures/site'), $o->getBasePath());
        $o->set('cssLibraries', 'bootstrap4 tailwind');
        self::assertSame(['bootstrap4', 'tailwind'], $o->getCssLibraries());
    }

    public function testPasswordsStayOutOfArguments(): void
    {
        $o = (new Options())->setPassword('open sesame', 'owner');
        self::assertNotContains('open sesame', $o->toArguments());
        self::assertSame(['HTML2PDF_USER_PASSWORD' => 'open sesame', 'HTML2PDF_OWNER_PASSWORD' => 'owner'], $o->passwordEnv());
    }

    public function testFormFields(): void
    {
        $f = (new Options())->setCssLibraries('bootstrap5,tailwind')->setPermissions(true, false)->toFormFields();
        self::assertSame(['bootstrap5', 'tailwind'], $f['cssLib']);
        self::assertSame('true', $f['denyCopy']);
        self::assertSame('false', $f['denyPrint']);
    }

    /**
     * @dataProvider invalid
     *
     * @param mixed $value
     */
    public function testValidation(string $name, $value): void
    {
        $this->expectException(Html2PdfException::class);
        (new Options())->set($name, $value);
    }

    /** @return array<string, array{0: string, 1: mixed}> */
    public function invalid(): array
    {
        return [
            'orientation' => ['orientation', 'sideways'],
            'margin' => ['marginMm', 2],
            'viewport' => ['viewportWidth', 100],
            'library' => ['cssLibraries', 'foundation'],
            'none and more' => ['cssLibraries', ['none', 'tailwind']],
            'base path' => ['basePath', __DIR__ . '/does-not-exist'],
            'unknown' => ['colour', 'red'],
        ];
    }
}
