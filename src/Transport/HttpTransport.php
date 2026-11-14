<?php

declare(strict_types=1);

namespace Html2Pdf\Transport;

use Html2Pdf\Exception\ConversionException;
use Html2Pdf\Exception\Html2PdfException;
use Html2Pdf\Exception\TimeoutException;
use Html2Pdf\Options;
use Html2Pdf\Result;

/**
 * Converts through a converter server (the binary started as a web
 * service: `html2pdf -addr :8080`), e.g. one shared container for several
 * applications. Uses PHP's own HTTP streams: no cURL needed.
 *
 * The server cannot read this machine's files: with a base path set, the
 * files the document refers to are uploaded with it (stylesheets, images
 * and fonts below the base path, as far as the HTML and its CSS mention
 * them). Remote resources need the server started with -allow-remote.
 */
final class HttpTransport implements TransportInterface
{
    /** @var string */
    private $url;
    /** @var array<string, string> */
    private $headers;

    /**
     * @param string                $baseUrl e.g. "http://pdf-service:8080"
     * @param array<string, string> $headers extra request headers (authentication for a proxy ...)
     */
    public function __construct(string $baseUrl, array $headers = [])
    {
        if (!preg_match('#^https?://#i', $baseUrl)) {
            throw new Html2PdfException("The converter URL must start with http:// or https://, got \"$baseUrl\"");
        }
        $this->url = rtrim($baseUrl, '/') . '/api/convert';
        $this->headers = $headers;
    }

    public function convert(string $html, string $documentName, Options $options): Result
    {
        $boundary = '----html2pdf' . bin2hex(random_bytes(12));
        $body = '';
        $files = [$documentName => $html];
        if ($options->getBasePath() !== null) {
            $files += AssetCollector::collect($options->getBasePath(), $documentName, $html);
        }
        foreach ($files as $path => $content) {
            $body .= self::part($boundary, 'file', $content, basename($path));
            $body .= self::part($boundary, 'path', $path);
        }
        $body .= self::part($boundary, 'main', $documentName);
        foreach ($options->toFormFields() as $name => $value) {
            foreach ((array) $value as $v) {
                $body .= self::part($boundary, $name, $v);
            }
        }
        $body .= "--$boundary--\r\n";

        $headers = ['Content-Type: multipart/form-data; boundary=' . $boundary, 'Accept: application/pdf'];
        foreach ($this->headers as $name => $value) {
            $headers[] = "$name: $value";
        }
        $context = stream_context_create(['http' => [
            'method' => 'POST',
            'header' => implode("\r\n", $headers),
            'content' => $body,
            'timeout' => $options->getTimeout(),
            'ignore_errors' => true, // read the JSON error of 4xx answers
        ]]);
        error_clear_last();
        $response = @file_get_contents($this->url, false, $context);
        // PHP 8.4 added http_get_last_response_headers(); $http_response_header is deprecated from 8.5
        if (function_exists('http_get_last_response_headers')) {
            $responseHeaders = http_get_last_response_headers() ?? [];
        } else {
            // not set at all when the connection failed, whatever PHPStan assumes
            // @phpstan-ignore-next-line
            $responseHeaders = isset($http_response_header) && is_array($http_response_header) ? $http_response_header : [];
        }
        if ($response === false || $responseHeaders === []) {
            $error = (string) (error_get_last()['message'] ?? '');
            if (stripos($error, 'timed out') !== false) {
                throw new TimeoutException("The converter at {$this->url} did not answer within {$options->getTimeout()} seconds");
            }
            throw new Html2PdfException("Could not reach the converter at {$this->url}" . ($error !== '' ? ': ' . $error : ''));
        }

        $status = 0;
        $head = [];
        foreach ($responseHeaders as $line) {
            if (preg_match('#^HTTP/\S+\s+(\d{3})#', $line, $m)) {
                $status = (int) $m[1]; // the last one counts after redirects
                $head = [];
            } elseif (strpos($line, ':') !== false) {
                [$name, $value] = explode(':', $line, 2);
                $head[strtolower(trim($name))] = trim($value);
            }
        }
        if ($status !== 200) {
            $error = json_decode($response, true);
            $message = is_array($error) && isset($error['error']) ? (string) $error['error'] : "HTTP $status";
            if ($status === 400 || $status === 413 || $status === 422) {
                throw new ConversionException($message);
            }
            throw new Html2PdfException("The converter answered $message");
        }

        $failures = json_decode(rawurldecode($head['x-missing-details'] ?? '[]'), true) ?: [];
        $libs = [];
        foreach (array_filter(array_map('trim', explode(',', $head['x-css-lib'] ?? ''))) as $entry) {
            [$lib, $source] = array_map('trim', explode(';', $entry, 2)) + [1 => ''];
            $libs[] = ['lib' => $lib, 'source' => $source];
        }
        return new Result($response, $libs, $failures, (int) ($head['x-render-time-ms'] ?? 0));
    }

    private static function part(string $boundary, string $name, string $content, ?string $filename = null): string
    {
        $disposition = 'form-data; name="' . $name . '"';
        $type = '';
        if ($filename !== null) {
            $disposition .= '; filename="' . str_replace(['"', "\r", "\n"], '_', $filename) . '"';
            $type = "Content-Type: application/octet-stream\r\n";
        }
        return "--$boundary\r\nContent-Disposition: $disposition\r\n$type\r\n$content\r\n";
    }
}
