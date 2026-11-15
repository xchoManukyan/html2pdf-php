<?php

declare(strict_types=1);

namespace Html2Pdf;

use Html2Pdf\Exception\Html2PdfException;

/**
 * Finds the converter binary for this machine and downloads it on first use.
 *
 * The binaries are the releases of the converter, html2pdf-core
 * (https://github.com/xchoManukyan/html2pdf-core). The package pins one
 * converter version: its SHA-256 checksums ship with the package
 * (resources/binaries.json), so a download is used only if it is exactly
 * that release's binary.
 *
 * Where the binary is looked for, in this order:
 *  1. the HTML2PDF_BINARY environment variable (a file you provide; not checked),
 *  2. the package's bin/ folder (put there by `vendor/bin/html2pdf-install`
 *     at deploy time, or by scripts/update-core.sh --local in a checkout),
 *  3. a cache folder: HTML2PDF_BINARY_DIR, or html2pdf-VERSION-USER in the
 *     system's temporary folder. That folder is created private (0700) and
 *     used only while it belongs to this user and nobody else can write to
 *     it, since other users share the temporary folder. Files there are
 *     checked against their checksum before the first use in a process.
 *
 * Environment variables: HTML2PDF_NO_DOWNLOAD=1 forbids downloading (the
 * binary must be installed beforehand), HTML2PDF_DOWNLOAD_URL sets a mirror,
 * e.g. "https://mirror.example.com/html2pdf/v{version}/{file}".
 */
final class BinaryInstaller
{
    public const DOWNLOAD_URL = 'https://github.com/xchoManukyan/html2pdf-core/releases/download/v{version}/{file}';

    /** @var array<string, string> paths verified in this process */
    private static $verified = [];

    /** @var array{version: string, files: array<string, array{sha256: string, size: int}>} */
    private $manifest;
    /** @var string */
    private $packageDir;
    /** @var string */
    private $cacheDir;
    /** @var string */
    private $downloadUrl;
    /** @var bool */
    private $downloadAllowed;
    /** @var bool the cache folder is the default one in the shared temporary folder */
    private $sharedTemp;

    /**
     * @param string|null $cacheDir    default: HTML2PDF_BINARY_DIR or the temporary folder
     * @param string|null $downloadUrl  default: HTML2PDF_DOWNLOAD_URL or the GitHub release
     * @param string|null $manifestPath default: the package's resources/binaries.json
     */
    public function __construct(?string $cacheDir = null, ?string $downloadUrl = null, ?bool $downloadAllowed = null, ?string $manifestPath = null)
    {
        $root = dirname(__DIR__);
        $this->manifest = self::readManifest($manifestPath ?? $root . '/resources/binaries.json');
        $this->packageDir = $root . DIRECTORY_SEPARATOR . 'bin';
        $configured = $cacheDir ?? self::env('HTML2PDF_BINARY_DIR');
        $this->sharedTemp = $configured === null;
        $this->cacheDir = $configured
            ?? sys_get_temp_dir() . DIRECTORY_SEPARATOR . 'html2pdf-' . $this->manifest['version'] . '-' . self::userTag();
        $this->downloadUrl = $downloadUrl ?? (self::env('HTML2PDF_DOWNLOAD_URL') ?? self::DOWNLOAD_URL);
        $this->downloadAllowed = $downloadAllowed ?? !in_array(strtolower((string) self::env('HTML2PDF_NO_DOWNLOAD')), ['1', 'true', 'yes'], true);
    }

    /** The version of the converter this package needs, e.g. "0.1.0". */
    public function version(): string
    {
        return $this->manifest['version'];
    }

    /**
     * The binary's file name for a platform, e.g. "html2pdf-linux-amd64".
     *
     * @param string|null $platform "linux-amd64", "darwin-arm64", "windows-amd64" ...; null: this machine
     */
    public static function fileName(?string $platform = null): string
    {
        if ($platform === null) {
            $os = strtolower(PHP_OS_FAMILY); // windows, darwin, linux, bsd ...
            $machine = strtolower(php_uname('m'));
            $arch = in_array($machine, ['aarch64', 'arm64', 'armv8', 'armv8l'], true) ? 'arm64' : 'amd64';
            $platform = "$os-$arch";
        }
        $platform = strtolower($platform);
        return 'html2pdf-' . $platform . (strpos($platform, 'windows-') === 0 ? '.exe' : '');
    }

    /** @return string[] the platforms with a published binary, e.g. "linux-amd64" */
    public function platforms(): array
    {
        return array_map(static function (string $file): string {
            return (string) preg_replace('/^html2pdf-|\.exe$/', '', $file);
        }, array_keys($this->manifest['files']));
    }

    /**
     * The path of a usable binary for this machine, downloading it if needed.
     */
    public function binary(): string
    {
        $explicit = self::env('HTML2PDF_BINARY');
        if ($explicit !== null) {
            if (!is_file($explicit)) {
                throw new Html2PdfException("HTML2PDF_BINARY names \"$explicit\", which does not exist");
            }
            return $explicit;
        }
        $file = self::fileName();
        $local = $this->packageDir . DIRECTORY_SEPARATOR . $file;
        if (is_file($local)) {
            return $local; // installed at deploy time or built from source
        }
        $cached = $this->cacheDir . DIRECTORY_SEPARATOR . $file;
        if ($this->sharedTemp) {
            $this->assertPrivateFolder($this->cacheDir);
        }
        if (is_file($cached) && $this->matches($cached, $file)) {
            return $cached;
        }
        if (!$this->downloadAllowed) {
            throw new Html2PdfException("The converter binary $file is not installed and downloading is turned off "
                . '(HTML2PDF_NO_DOWNLOAD). Run "vendor/bin/html2pdf-install" when deploying, or set HTML2PDF_BINARY.');
        }
        return $this->install(null, is_writable($this->packageDir) ? $this->packageDir : $this->cacheDir);
    }

    /**
     * Downloads and verifies the binary for a platform into a folder.
     *
     * @param string|null $platform e.g. "linux-amd64"; null: this machine
     * @param string|null $dir      default: the package's bin/ folder
     *
     * @return string the installed file
     */
    public function install(?string $platform = null, ?string $dir = null): string
    {
        $file = self::fileName($platform);
        if (!isset($this->manifest['files'][$file])) {
            throw new Html2PdfException("No converter binary is published for this platform ($file). Published: "
                . implode(', ', $this->platforms()) . '. Build one from html2pdf-core (scripts/build.sh --host) and set HTML2PDF_BINARY.');
        }
        $dir = $dir ?? $this->packageDir;
        if (!is_dir($dir) && !@mkdir($dir, 0700, true) && !is_dir($dir)) {
            throw new Html2PdfException("Cannot create the folder \"$dir\" for the converter binary");
        }
        $target = $dir . DIRECTORY_SEPARATOR . $file;
        if (is_file($target) && $this->matches($target, $file)) {
            return $target;
        }

        $url = strtr($this->downloadUrl, ['{version}' => $this->manifest['version'], '{file}' => $file]);
        $data = self::download($url, $this->manifest['files'][$file]['size']);
        $expected = $this->manifest['files'][$file]['sha256'];
        if (!hash_equals($expected, hash('sha256', $data))) {
            throw new Html2PdfException("The converter downloaded from $url does not match its checksum; it was not installed");
        }

        // write next to the target, then rename: atomic, so a parallel request never runs half a file
        $tmp = $target . '.' . bin2hex(random_bytes(6)) . '.tmp';
        if (@file_put_contents($tmp, $data) !== strlen($data)) {
            @unlink($tmp);
            throw new Html2PdfException("Cannot write the converter binary to \"$dir\"");
        }
        @chmod($tmp, 0755);
        if (!@rename($tmp, $target)) {
            @unlink($tmp);
            if (!(is_file($target) && $this->matches($target, $file))) { // another process may have won the race
                throw new Html2PdfException("Cannot move the converter binary into \"$dir\"");
            }
        }
        self::$verified[$target] = $expected;
        return $target;
    }

    /** Whether $path is the published binary $file (checked once per process). */
    private function matches(string $path, string $file): bool
    {
        $expected = $this->manifest['files'][$file]['sha256'] ?? null;
        if ($expected === null) {
            return false;
        }
        if ((self::$verified[$path] ?? null) === $expected) {
            return true;
        }
        $actual = @hash_file('sha256', $path);
        if ($actual === false || !hash_equals($expected, $actual)) {
            return false;
        }
        self::$verified[$path] = $expected;
        return true;
    }

    private static function download(string $url, int $expectedSize): string
    {
        if (!preg_match('#^https?://#i', $url) && !preg_match('#^file://#i', $url)) {
            throw new Html2PdfException("Unsupported download URL \"$url\"");
        }
        if (stripos($url, 'https://') === 0 && !extension_loaded('openssl') && !function_exists('curl_init')) {
            throw new Html2PdfException('Downloading the converter needs the openssl or curl extension; '
                . 'or install the binary yourself and set HTML2PDF_BINARY');
        }
        $limit = max(0, $expectedSize) + 1024;
        if (function_exists('curl_init') && stripos($url, 'file://') !== 0) {
            [$data, $errno, $error] = self::curlGet($url, []);
            // An outdated CA bundle in php.ini (common in Windows PHP bundles such as
            // Laragon or XAMPP) cannot verify GitHub's download host: try the
            // operating system's certificate store, then Composer's bundle. The
            // certificate is still verified, and the file by its SHA-256 checksum.
            foreach (self::caFallbacks() as $options) {
                if (is_string($data) || !in_array($errno, [60, 77], true)) {
                    break;
                }
                [$data, $errno, $error] = self::curlGet($url, $options);
            }
            if (!is_string($data)) {
                throw new Html2PdfException("Could not download the converter from $url: $error"
                    . (in_array($errno, [60, 77], true) ? '. The CA certificates PHP uses are outdated: update the file set '
                        . 'as curl.cainfo in php.ini (https://curl.se/ca/cacert.pem), or install the binary with '
                        . 'vendor/bin/html2pdf-install on a machine that can download it' : ''));
            }
        } else {
            $context = stream_context_create(['http' => [
                'timeout' => 300,
                'follow_location' => 1,
                'max_redirects' => 5,
                'user_agent' => 'html2pdf-php',
            ]]);
            $data = @file_get_contents($url, false, $context, 0, $limit);
            if ($data === false) {
                $error = error_get_last();
                throw new Html2PdfException("Could not download the converter from $url" . ($error ? ': ' . $error['message'] : ''));
            }
        }
        if (strlen($data) > $limit) {
            throw new Html2PdfException("The download from $url is larger than expected");
        }
        return $data;
    }

    /**
     * @param array<int, mixed> $extra curl options
     *
     * @return array{0: string|false, 1: int, 2: string} data, curl error number, error message
     */
    private static function curlGet(string $url, array $extra): array
    {
        $ch = curl_init($url);
        if ($ch === false) {
            return [false, 0, 'curl_init failed'];
        }
        curl_setopt_array($ch, $extra + [
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_FOLLOWLOCATION => true,
            CURLOPT_MAXREDIRS => 5,
            CURLOPT_CONNECTTIMEOUT => 15,
            CURLOPT_TIMEOUT => 300,
            CURLOPT_FAILONERROR => true,
            CURLOPT_USERAGENT => 'html2pdf-php',
        ]);
        $data = curl_exec($ch);
        $result = [is_string($data) ? $data : false, curl_errno($ch), curl_error($ch)];
        curl_close($ch);
        return $result;
    }

    /** Distinguishes users sharing the temporary folder. */
    private static function userTag(): string
    {
        if (function_exists('posix_geteuid')) {
            return (string) posix_geteuid();
        }
        return substr(hash('sha256', (string) get_current_user() . '|' . __DIR__), 0, 12);
    }

    /**
     * Makes sure a folder in the shared temporary folder is ours alone, so
     * nobody else can replace the binary in it between the checksum check and
     * its use. Creates it with 0700 permissions when missing.
     */
    private function assertPrivateFolder(string $dir): void
    {
        if (!is_dir($dir) && !@mkdir($dir, 0700, true) && !is_dir($dir)) {
            throw new Html2PdfException("Cannot create the folder \"$dir\" for the converter binary");
        }
        if (DIRECTORY_SEPARATOR === '\\') {
            return; // Windows: the temporary folder is per user
        }
        clearstatcache(true, $dir);
        $perms = @fileperms($dir);
        $owner = @fileowner($dir);
        // without the posix extension: only the owner can write (checked below), so being able to write means ours
        $ownedByUs = function_exists('posix_geteuid') ? $owner === posix_geteuid() : is_writable($dir);
        if (is_link($dir) || $perms === false || ($perms & 0022) !== 0 || !$ownedByUs) {
            throw new Html2PdfException("The converter cache folder \"$dir\" belongs to another user or others can "
                . 'write to it, so it is not used. Install the binary with vendor/bin/html2pdf-install, or set '
                . 'HTML2PDF_BINARY_DIR to a folder only this user can write to.');
        }
    }

    /**
     * Other trusted certificate sources to try when the configured CA bundle
     * cannot verify the server.
     *
     * @return array<int, array<int, mixed>>
     */
    private static function caFallbacks(): array
    {
        $fallbacks = [];
        $version = curl_version();
        // CURLSSLOPT_NATIVE_CA (curl 7.71+): the operating system's certificate store
        if (is_array($version) && version_compare((string) $version['version'], '7.71.0', '>=')) {
            $fallbacks[] = [CURLOPT_SSL_OPTIONS => 16];
        }
        // composer/ca-bundle, present in many projects (Composer itself uses it)
        $caBundle = 'Composer\\CaBundle\\CaBundle';
        if (class_exists($caBundle)) {
            $path = $caBundle::getBundledCaBundlePath();
            if (is_string($path) && is_file($path)) {
                $fallbacks[] = [CURLOPT_CAINFO => $path];
            }
        }
        return $fallbacks;
    }

    /** @return array{version: string, files: array<string, array{sha256: string, size: int}>} */
    private static function readManifest(string $path): array
    {
        $json = @file_get_contents($path);
        $manifest = $json === false ? null : json_decode($json, true);
        if (!is_array($manifest) || !isset($manifest['version'], $manifest['files']) || !is_array($manifest['files'])) {
            throw new Html2PdfException("The binary manifest \"$path\" is missing or broken; reinstall the package");
        }
        /** @var array{version: string, files: array<string, array{sha256: string, size: int}>} $manifest */
        return $manifest;
    }

    private static function env(string $name): ?string
    {
        $value = getenv($name);
        return $value === false || $value === '' ? null : $value;
    }
}
