<?php

declare(strict_types=1);

namespace Html2Pdf\Tests;

use Html2Pdf\BinaryInstaller;
use Html2Pdf\Exception\Html2PdfException;
use Html2Pdf\Html2Pdf;
use PHPUnit\Framework\TestCase;

final class BinaryInstallerTest extends TestCase
{
    /** @var string */
    private $dir;

    protected function setUp(): void
    {
        $this->dir = sys_get_temp_dir() . '/h2p-installer-' . bin2hex(random_bytes(4));
        mkdir($this->dir . '/release', 0777, true);
    }

    protected function tearDown(): void
    {
        foreach (new \RecursiveIteratorIterator(new \RecursiveDirectoryIterator($this->dir, \FilesystemIterator::SKIP_DOTS),
            \RecursiveIteratorIterator::CHILD_FIRST) as $f) {
            $f->isDir() ? rmdir($f->getPathname()) : unlink($f->getPathname());
        }
        rmdir($this->dir);
    }

    /** A fake release with one binary, and the manifest that lists it. */
    private function release(string $content, string $listedContent = null): BinaryInstaller
    {
        $file = 'html2pdf-linux-amd64';
        file_put_contents("{$this->dir}/release/$file", $content);
        $listed = $listedContent ?? $content;
        file_put_contents("{$this->dir}/binaries.json", json_encode(['version' => '9.9.9', 'files' => [
            $file => ['sha256' => hash('sha256', $listed), 'size' => strlen($listed)],
        ]]));
        $url = 'file://' . str_replace('\\', '/', $this->dir) . '/release/{file}';
        return new BinaryInstaller("{$this->dir}/cache", $url, true, "{$this->dir}/binaries.json");
    }

    public function testDownloadsAndVerifies(): void
    {
        $installer = $this->release('binary bytes');
        $path = $installer->install('linux-amd64', "{$this->dir}/target");
        self::assertSame('binary bytes', file_get_contents($path));
        self::assertSame('html2pdf-linux-amd64', basename($path));
        self::assertSame([], glob("{$this->dir}/target/*.tmp"), 'no temporary file left behind');
        // a second install keeps the verified file
        self::assertSame($path, $installer->install('linux-amd64', "{$this->dir}/target"));
    }

    public function testRejectsAModifiedDownload(): void
    {
        $installer = $this->release('tampered bytes', 'binary bytes');
        try {
            $installer->install('linux-amd64', "{$this->dir}/target");
            self::fail('a binary that does not match its checksum must not be installed');
        } catch (Html2PdfException $e) {
            self::assertStringContainsString('checksum', $e->getMessage());
        }
        self::assertFileDoesNotExist("{$this->dir}/target/html2pdf-linux-amd64");
    }

    public function testReplacesAModifiedInstalledFile(): void
    {
        $installer = $this->release('binary bytes');
        mkdir("{$this->dir}/target");
        file_put_contents("{$this->dir}/target/html2pdf-linux-amd64", 'something else');
        $installer->install('linux-amd64', "{$this->dir}/target");
        self::assertSame('binary bytes', file_get_contents("{$this->dir}/target/html2pdf-linux-amd64"));
    }

    public function testUnknownPlatform(): void
    {
        $this->expectException(Html2PdfException::class);
        $this->expectExceptionMessage('No converter binary is published');
        $this->release('x')->install('plan9-amd64', "{$this->dir}/target");
    }

    public function testFileNames(): void
    {
        self::assertSame('html2pdf-windows-arm64.exe', BinaryInstaller::fileName('windows-arm64'));
        self::assertSame('html2pdf-darwin-arm64', BinaryInstaller::fileName('Darwin-ARM64'));
        self::assertMatchesRegularExpression('/^html2pdf-[a-z]+-(amd64|arm64)(\.exe)?$/', BinaryInstaller::fileName());
    }

    public function testPackageManifest(): void
    {
        $installer = new BinaryInstaller();
        self::assertSame(Html2Pdf::VERSION, $installer->version(), 'resources/binaries.json belongs to this version');
        self::assertSame(['linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64', 'windows-amd64', 'windows-arm64'],
            $installer->platforms());
    }
}
