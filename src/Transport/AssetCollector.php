<?php

declare(strict_types=1);

namespace Html2Pdf\Transport;

/**
 * Finds the local files a document refers to (stylesheets, images, fonts,
 * and what those stylesheets refer to), so they can be sent to a converter
 * server along with the document. Only files inside the base path are read.
 *
 * @internal
 */
final class AssetCollector
{
    /** Uploads stop here; the server accepts 50 MB in total. */
    private const MAX_TOTAL = 45 << 20;

    private const HTML_REFS = '/\b(?:href|src|poster|data-src)\s*=\s*(?:"([^"]*)"|\'([^\']*)\'|([^\s>"\']+))/i';
    private const SRCSET = '/\bsrcset\s*=\s*(?:"([^"]*)"|\'([^\']*)\')/i';
    private const CSS_REFS = '/url\(\s*(?:"([^"]*)"|\'([^\']*)\'|([^)"\'\s]*))\s*\)|@import\s+(?:"([^"]*)"|\'([^\']*)\')/i';
    private const STYLE_BLOCKS = '/<style\b[^>]*>(.*?)<\/style>|\bstyle\s*=\s*(?:"([^"]*)"|\'([^\']*)\')/is';

    /**
     * @return array<string, string> file contents by path relative to $basePath
     */
    public static function collect(string $basePath, string $documentName, string $html): array
    {
        $root = rtrim(str_replace('\\', '/', (string) realpath($basePath)), '/');
        $files = [];
        $total = 0;
        $queue = [];

        $refs = self::matches(self::HTML_REFS, $html);
        foreach (self::matches(self::SRCSET, $html) as $set) {
            foreach (explode(',', $set) as $candidate) {
                $refs[] = preg_split('/\s+/', trim($candidate))[0] ?? '';
            }
        }
        foreach (self::matches(self::STYLE_BLOCKS, $html) as $css) {
            $refs = array_merge($refs, self::matches(self::CSS_REFS, $css));
        }
        foreach ($refs as $ref) {
            $queue[] = [$documentName, $ref];
        }

        while ($queue) {
            [$from, $ref] = array_shift($queue);
            $path = self::resolve($from, $ref);
            if ($path === null || isset($files[$path])) {
                continue;
            }
            $real = realpath($root . '/' . $path);
            if ($real === false || !is_file($real)) {
                continue; // the converter reports it as missing
            }
            $real = str_replace('\\', '/', $real);
            if (strpos($real, $root . '/') !== 0) {
                continue; // outside the base path (a link pointing elsewhere)
            }
            $size = filesize($real);
            if ($size === false || $total + $size > self::MAX_TOTAL) {
                continue;
            }
            $content = file_get_contents($real);
            if ($content === false) {
                continue;
            }
            $files[$path] = $content;
            $total += $size;
            if (preg_match('/\.css$/i', $path)) {
                foreach (self::matches(self::CSS_REFS, $content) as $inner) {
                    $queue[] = [$path, $inner];
                }
            }
        }
        unset($files[$documentName]);
        return $files;
    }

    /**
     * Resolves $ref relative to the file $from (both relative to the base
     * path); null for URLs, data: URIs, fragments and empty references.
     */
    private static function resolve(string $from, string $ref): ?string
    {
        $ref = trim(html_entity_decode($ref, ENT_QUOTES | ENT_HTML5, 'UTF-8'));
        $ref = preg_replace('/[?#].*$/s', '', $ref) ?? '';
        if ($ref === '' || preg_match('#^([a-z][a-z0-9+.-]*:|//)#i', $ref)) {
            return null;
        }
        $ref = rawurldecode($ref);
        $slash = strrpos($from, '/');
        $dir = $ref[0] === '/' || $slash === false ? '' : substr($from, 0, $slash); // not dirname(): it uses \ on Windows
        $parts = [];
        foreach (explode('/', $dir . '/' . $ref) as $part) {
            if ($part === '' || $part === '.') {
                continue;
            }
            if ($part === '..') {
                if (!$parts) {
                    return null; // above the base path
                }
                array_pop($parts);
                continue;
            }
            $parts[] = $part;
        }
        return $parts ? implode('/', $parts) : null;
    }

    /** @return string[] the first non-empty group of every match */
    private static function matches(string $pattern, string $text): array
    {
        if (!preg_match_all($pattern, $text, $all, PREG_SET_ORDER)) {
            return [];
        }
        $out = [];
        foreach ($all as $m) {
            for ($i = 1; $i < count($m); $i++) {
                if ($m[$i] !== '') {
                    $out[] = $m[$i];
                    break;
                }
            }
        }
        return $out;
    }
}
