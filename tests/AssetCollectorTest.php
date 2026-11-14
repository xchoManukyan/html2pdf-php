<?php

declare(strict_types=1);

namespace Html2Pdf\Tests;

use Html2Pdf\Transport\AssetCollector;
use PHPUnit\Framework\TestCase;

final class AssetCollectorTest extends TestCase
{
    public function testFollowsHtmlAndCss(): void
    {
        $site = __DIR__ . '/fixtures/site';
        $files = AssetCollector::collect($site, 'pages/sub.html', (string) file_get_contents("$site/pages/sub.html"));
        self::assertSame(['css/style.css', 'img/logo.svg'], array_keys($files), 'the stylesheet, then the image it uses');
    }

    public function testStaysInsideTheBasePath(): void
    {
        $site = __DIR__ . '/fixtures/site';
        $html = '<img src="../../OptionsTest.php"><img src="/img/logo.svg?v=2#x"><img src="https://example.com/a.png">'
            . '<img src="data:image/png;base64,AA=="><div style="background:url(\'img/logo.svg\')"></div>'
            . '<img srcset="img/logo.svg 1x, img/nope.png 2x">';
        self::assertSame(['img/logo.svg'], array_keys(AssetCollector::collect($site, 'index.html', $html)));
    }
}
