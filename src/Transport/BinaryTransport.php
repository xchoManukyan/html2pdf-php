<?php

declare(strict_types=1);

namespace Html2Pdf\Transport;

use Html2Pdf\BinaryInstaller;
use Html2Pdf\Exception\ConversionException;
use Html2Pdf\Exception\Html2PdfException;
use Html2Pdf\Exception\TimeoutException;
use Html2Pdf\Options;
use Html2Pdf\Result;

/**
 * Converts with the converter binary, run as a child process. By default the
 * binary for this machine (Linux, macOS or Windows, x64 or ARM64) is found or
 * downloaded and verified by BinaryInstaller; another one can be given to
 * the constructor or in the HTML2PDF_BINARY environment variable.
 *
 * The document goes in and the PDF comes out through temporary files, so
 * nothing blocks on pipe buffers and it works the same on every system.
 */
final class BinaryTransport implements TransportInterface
{
    /** @var string */
    private $binary;

    public function __construct(?string $binary = null)
    {
        $binary = $binary ?? (new BinaryInstaller())->binary();
        if (!is_file($binary)) {
            throw new Html2PdfException("The converter binary \"$binary\" does not exist");
        }
        if (DIRECTORY_SEPARATOR === '/' && !is_executable($binary)) {
            @chmod($binary, 0755); // archives can lose the executable bit
        }
        $this->binary = $binary;
    }

    public function getBinary(): string
    {
        return $this->binary;
    }

    /** The binary's version, e.g. "0.1.0". */
    public function version(): string
    {
        [$code, $out, $err] = $this->run(['-version'], null, [], 10);
        if ($code !== 0) {
            throw new Html2PdfException('The converter binary does not run: ' . trim($err ?: $out));
        }
        return trim($out);
    }

    public function convert(string $html, string $documentName, Options $options): Result
    {
        $pdfFile = $this->tempFile();
        try {
            $args = array_merge(['-in', '-', '-out', $pdfFile, '-json', '-doc', $documentName], $options->toArguments());
            if ($options->getBasePath() !== null) {
                array_push($args, '-root', $options->getBasePath());
            }
            [$code, $out, $err] = $this->run($args, $html, $options->passwordEnv(), $options->getTimeout());

            $report = json_decode($out, true);
            if (!is_array($report)) {
                throw new Html2PdfException("The converter failed (exit code $code): " . trim($err ?: $out));
            }
            if (empty($report['ok'])) {
                throw new ConversionException((string) ($report['error'] ?? 'conversion failed'));
            }
            $pdf = file_get_contents($pdfFile);
            if ($pdf === false || $pdf === '') {
                throw new Html2PdfException('The converter did not write a PDF');
            }
            return new Result($pdf, $report['cssLibs'] ?? [], $report['failures'] ?? [], (int) ($report['tookMs'] ?? 0));
        } finally {
            @unlink($pdfFile);
        }
    }

    /**
     * Runs the binary with $stdin as standard input.
     *
     * @param string[]              $args
     * @param array<string, string> $env  added to the inherited environment
     *
     * @return array{0: int, 1: string, 2: string} exit code, stdout, stderr
     */
    private function run(array $args, ?string $stdin, array $env, int $timeout): array
    {
        $in = $this->tempFile();
        $out = $this->tempFile();
        $err = $this->tempFile();
        try {
            if (file_put_contents($in, (string) $stdin) === false) {
                throw new Html2PdfException('Could not write a temporary file in ' . sys_get_temp_dir());
            }
            $descriptors = [0 => ['file', $in, 'r'], 1 => ['file', $out, 'w'], 2 => ['file', $err, 'w']];
            $environment = $env ? array_merge(getenv(), $env) : null;
            $process = proc_open(array_merge([$this->binary], $args), $descriptors, $pipes, null, $environment, ['bypass_shell' => true]);
            if (!is_resource($process)) {
                throw new Html2PdfException("Could not start the converter \"{$this->binary}\"");
            }

            $deadline = microtime(true) + $timeout;
            $sleep = 1000;
            $code = -1;
            while (true) {
                $status = proc_get_status($process);
                if ($status === false || !$status['running']) {
                    $code = $status === false ? -1 : (int) $status['exitcode'];
                    break;
                }
                if (microtime(true) > $deadline) {
                    proc_terminate($process, 9);
                    proc_close($process);
                    throw new TimeoutException("The conversion took longer than $timeout seconds");
                }
                usleep($sleep);
                $sleep = min($sleep * 2, 50000);
            }
            proc_close($process);
            return [$code, (string) file_get_contents($out), (string) file_get_contents($err)];
        } finally {
            @unlink($in);
            @unlink($out);
            @unlink($err);
        }
    }

    private function tempFile(): string
    {
        $file = tempnam(sys_get_temp_dir(), 'h2p');
        if ($file === false) {
            throw new Html2PdfException('Could not create a temporary file in ' . sys_get_temp_dir());
        }
        return $file;
    }
}
