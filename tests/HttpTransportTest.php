<?php

declare(strict_types=1);

namespace Html2Pdf\Tests;

use Html2Pdf\Exception\Html2PdfException;
use Html2Pdf\Html2Pdf;
use Html2Pdf\Options;
use Html2Pdf\Transport\BinaryTransport;
use Html2Pdf\Transport\HttpTransport;
use Html2Pdf\Transport\TransportInterface;

/**
 * Runs the conversion tests against the converter started as a server.
 */
final class HttpTransportTest extends ConversionTestCase
{
    /** @var resource|null */
    private static $server;
    /** @var string */
    private static $url = '';

    public static function setUpBeforeClass(): void
    {
        $socket = stream_socket_server('tcp://127.0.0.1:0');
        if ($socket === false) {
            self::fail('no free port for the converter server');
        }
        $port = (int) substr((string) strrchr((string) stream_socket_get_name($socket, false), ':'), 1);
        fclose($socket);
        self::$url = "http://127.0.0.1:$port";
        $null = DIRECTORY_SEPARATOR === '\\' ? 'NUL' : '/dev/null';
        $server = proc_open([(new BinaryTransport())->getBinary(), '-addr', "127.0.0.1:$port",
            '-sign-cert', __DIR__ . '/fixtures/signing/test-cert.pem', '-sign-key', __DIR__ . '/fixtures/signing/test-key.pem'],
            [0 => ['file', $null, 'r'], 1 => ['file', $null, 'w'], 2 => ['file', $null, 'w']], $pipes);
        if ($server === false) {
            self::fail('the converter server could not be started');
        }
        self::$server = $server;
        for ($i = 0; $i < 100; $i++) {
            $conn = @fsockopen('127.0.0.1', $port, $errno, $errstr, 0.1);
            if ($conn) {
                fclose($conn);
                return;
            }
            usleep(50000);
        }
        self::fail('the converter server did not start');
    }

    public static function tearDownAfterClass(): void
    {
        if (is_resource(self::$server)) {
            proc_terminate(self::$server);
            proc_close(self::$server);
        }
    }

    protected function transport(): TransportInterface
    {
        return new HttpTransport(self::$url);
    }

    protected function signing(): Options
    {
        return (new Options())->setSignature(Options::SERVER_CERTIFICATE, null, ['reason' => 'Approved']);
    }

    public function testUnreachableServer(): void
    {
        $this->expectException(Html2PdfException::class);
        $this->expectExceptionMessage('Could not reach');
        (new Html2Pdf(['timeout' => 2], new HttpTransport('http://127.0.0.1:1')))->loadHtml('<p>x</p>')->render();
    }
}
