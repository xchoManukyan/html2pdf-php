<?php

declare(strict_types=1);

namespace Html2Pdf;

use Html2Pdf\Exception\Html2PdfException;

/**
 * Conversion settings. Setters return $this, so they chain:
 *
 *     $options = (new Options())->setPaperSize('A4')->setOrientation('landscape')->setRemoteEnabled(true);
 *
 * The dompdf option names used most often also work with set() and the
 * constructor array (isRemoteEnabled, chroot, defaultPaperSize,
 * defaultPaperOrientation), which makes migrating from dompdf easier.
 */
final class Options
{
    public const PORTRAIT = 'portrait';
    public const LANDSCAPE = 'landscape';

    public const TAILWIND = 'tailwind';
    public const BOOTSTRAP5 = 'bootstrap5';
    public const BOOTSTRAP4 = 'bootstrap4';
    public const NO_LIBRARY = 'none';

    public const AES256 = 'aes-256';
    public const AES128 = 'aes-128';
    public const RC4128 = 'rc4-128';

    /** setSignature() with HttpTransport: the converter server signs with its own certificate. */
    public const SERVER_CERTIFICATE = '';

    private const PAPER_SIZES = ['A3', 'A4', 'A5', 'LETTER', 'LEGAL'];
    private const LIBRARIES = [self::TAILWIND, self::BOOTSTRAP5, self::BOOTSTRAP4, self::NO_LIBRARY];

    /** @var string A3, A4, A5, Letter, Legal or "WIDTHxHEIGHT" in millimetres */
    private $paperSize = 'A4';
    /** @var string */
    private $orientation = self::PORTRAIT;
    /** @var float */
    private $marginMm = 15.0;
    /** @var bool */
    private $pageNumbers = false;
    /** @var int|null */
    private $viewportWidth;
    /** @var string[] empty: detect the libraries from the page */
    private $cssLibraries = [];
    /** @var bool */
    private $remoteEnabled = false;
    /** @var string|null */
    private $basePath;
    /** @var string|null */
    private $userPassword;
    /** @var string|null */
    private $ownerPassword;
    /** @var bool */
    private $allowPrint = true;
    /** @var bool */
    private $allowCopy = true;
    /** @var bool */
    private $allowModify = true;
    /** @var int */
    private $timeout = 60;
    /** @var string */
    private $encryption = self::AES256;
    /** @var bool */
    private $pdfA = false;
    /** @var bool */
    private $fillableForms = false;
    /** @var array{cert: string, key: string, reason: string, location: string, contact: string, name: string}|null */
    private $signature;

    /**
     * @param array<string, mixed> $options e.g. ['paperSize' => 'Letter', 'isRemoteEnabled' => true]
     */
    public function __construct(array $options = [])
    {
        foreach ($options as $name => $value) {
            $this->set($name, $value);
        }
    }

    /**
     * Sets an option by name: any setter's name without "set"
     * ("paperSize", "remoteEnabled" ...) or a dompdf option name.
     *
     * @param mixed $value
     */
    public function set(string $name, $value): self
    {
        $aliases = [
            'isremoteenabled' => 'remoteEnabled',
            'chroot' => 'basePath',
            'defaultpapersize' => 'paperSize',
            'defaultpaperorientation' => 'orientation',
            'margin' => 'marginMm',
            'csslibrary' => 'cssLibraries',
            'csslib' => 'cssLibraries',
            'forms' => 'fillableForms',
            'activeforms' => 'fillableForms', // mPDF: useActiveForms
            'useactiveforms' => 'fillableForms',
        ];
        $key = strtolower(str_replace(['_', '-'], '', $name));
        $name = $aliases[$key] ?? $name;
        $setter = 'set' . ucfirst($name);
        if (!method_exists($this, $setter) || $setter === 'set') {
            throw new Html2PdfException("Unknown option \"$name\"");
        }
        $this->$setter($value);
        return $this;
    }

    /**
     * A3, A4, A5, Letter or Legal, or a custom size: [width, height] in
     * millimetres or "210x297".
     *
     * @param string|array<int, float|int> $size
     */
    public function setPaperSize($size): self
    {
        if (is_array($size)) {
            if (count($size) !== 2 || !isset($size[0], $size[1])) {
                throw new Html2PdfException('A custom paper size is [width, height] in millimetres');
            }
            $size = self::millimetres($size[0]) . 'x' . self::millimetres($size[1]);
        }
        $upper = strtoupper(trim((string) $size));
        if (!in_array($upper, self::PAPER_SIZES, true) && !preg_match('/^\d{2,4}(\.\d+)?X\d{2,4}(\.\d+)?$/', $upper)) {
            throw new Html2PdfException("Unsupported paper size \"$size\": use A3, A4, A5, Letter, Legal or WIDTHxHEIGHT in mm (10–9999)");
        }
        $this->paperSize = $upper;
        return $this;
    }

    public function setOrientation(string $orientation): self
    {
        $orientation = strtolower(trim($orientation));
        if ($orientation !== self::PORTRAIT && $orientation !== self::LANDSCAPE) {
            throw new Html2PdfException("Orientation must be \"portrait\" or \"landscape\", got \"$orientation\"");
        }
        $this->orientation = $orientation;
        return $this;
    }

    /** Page margin on every side in millimetres (5–50). CSS @page rules in the document win. */
    public function setMarginMm(float $mm): self
    {
        if ($mm < 5 || $mm > 50) {
            throw new Html2PdfException('The margin must be between 5 and 50 mm; use CSS @page { margin: ... } for other values');
        }
        $this->marginMm = $mm;
        return $this;
    }

    /** Print "Page X of Y" at the bottom of every page. */
    public function setPageNumbers(bool $enabled = true): self
    {
        $this->pageNumbers = $enabled;
        return $this;
    }

    /**
     * Lay the page out as in a browser window this many CSS pixels wide,
     * then scale it to the paper: 1280 prints a site's desktop layout, 375
     * its mobile one. null: the paper width, as when printing.
     */
    public function setViewportWidth(?int $px): self
    {
        if ($px !== null && ($px < 240 || $px > 3840)) {
            throw new Html2PdfException('The viewport width must be between 240 and 3840 px');
        }
        $this->viewportWidth = $px;
        return $this;
    }

    /**
     * CSS libraries to apply: any of Options::TAILWIND, BOOTSTRAP5,
     * BOOTSTRAP4, or Options::NO_LIBRARY alone. Empty (the default):
     * detect them from the page; several can be found.
     *
     * @param string|string[] $libraries
     */
    public function setCssLibraries($libraries): self
    {
        if (is_string($libraries)) {
            $libraries = preg_split('/[\s,;]+/', $libraries, -1, PREG_SPLIT_NO_EMPTY) ?: [];
        }
        $list = [];
        foreach ($libraries as $lib) {
            $lib = strtolower(trim((string) $lib));
            if ($lib === 'auto' || $lib === '') {
                continue;
            }
            if ($lib === 'bootstrap') {
                $lib = self::BOOTSTRAP5;
            }
            if (!in_array($lib, self::LIBRARIES, true)) {
                throw new Html2PdfException("Unknown CSS library \"$lib\": use tailwind, bootstrap5, bootstrap4 or none");
            }
            $list[$lib] = true;
        }
        if (isset($list[self::NO_LIBRARY]) && count($list) > 1) {
            throw new Html2PdfException('"none" cannot be combined with other CSS libraries');
        }
        $this->cssLibraries = array_keys($list);
        return $this;
    }

    /** Allow downloading http(s) images, stylesheets and fonts (off by default). */
    public function setRemoteEnabled(bool $enabled = true): self
    {
        $this->remoteEnabled = $enabled;
        return $this;
    }

    /**
     * Folder the document may load local files from (stylesheets, images,
     * fonts); relative links resolve against the document inside it.
     * Nothing outside it can be read (like dompdf's chroot).
     */
    public function setBasePath(?string $path): self
    {
        if ($path !== null) {
            $real = realpath($path);
            if ($real === false || !is_dir($real)) {
                throw new Html2PdfException("The base path \"$path\" is not a folder");
            }
            $path = $real;
        }
        $this->basePath = $path;
        return $this;
    }

    /**
     * Protect the PDF: $user is needed to open it, $owner gives full access
     * (random when null).
     */
    public function setPassword(?string $user, ?string $owner = null): self
    {
        $this->userPassword = $user === '' ? null : $user;
        $this->ownerPassword = $owner === '' ? null : $owner;
        return $this;
    }

    /** What readers may do without the owner password; restricting anything encrypts the PDF. */
    public function setPermissions(bool $print = true, bool $copy = true, bool $modify = true): self
    {
        $this->allowPrint = $print;
        $this->allowCopy = $copy;
        $this->allowModify = $modify;
        return $this;
    }

    /**
     * Encryption of protected PDFs (with a password or restricted
     * permissions): Options::AES256 (default, readable by Acrobat X+ and all
     * current viewers), Options::AES128, or Options::RC4128 for very old readers.
     */
    public function setEncryption(string $method): self
    {
        $method = strtolower(trim($method));
        if (!in_array($method, [self::AES256, self::AES128, self::RC4128], true)) {
            throw new Html2PdfException("Unknown encryption \"$method\": use aes-256, aes-128 or rc4-128");
        }
        $this->encryption = $method;
        return $this;
    }

    /**
     * Write PDF/A-2b, the archival format (embedded fonts, XMP metadata, sRGB
     * output intent). It cannot be combined with passwords or permissions,
     * and needs fonts the converter can embed: installed fonts or @font-face.
     */
    public function setPdfA(bool $enabled = true): self
    {
        $this->pdfA = $enabled;
        return $this;
    }

    /**
     * Turn inputs, checkboxes, radio buttons, selects and textareas into PDF
     * form fields people can fill in (otherwise they are printed as they look).
     */
    public function setFillableForms(bool $enabled = true): self
    {
        $this->fillableForms = $enabled;
        return $this;
    }

    /**
     * Sign the PDF digitally (PAdES baseline signature, SHA-256) with a
     * certificate and its private key: PEM files, RSA or ECDSA, the key
     * unencrypted (both may be in one file). $details: reason, location,
     * contact, name. null removes the signature.
     *
     * With HttpTransport the converter server signs with its own certificate
     * (started with -sign-cert and -sign-key): pass Options::SERVER_CERTIFICATE;
     * only the details are sent.
     *
     * @param array<string, string> $details reason, location, contact and/or name
     */
    public function setSignature(?string $certificate, ?string $privateKey = null, array $details = []): self
    {
        if ($certificate === null) {
            $this->signature = null;
            return $this;
        }
        $privateKey = $privateKey ?? $certificate;
        foreach ([$certificate, $privateKey] as $file) {
            if ($file !== '' && !is_readable($file)) {
                throw new Html2PdfException("Cannot read the signing file \"$file\"");
            }
        }
        foreach (array_keys($details) as $key) {
            if (!in_array($key, ['reason', 'location', 'contact', 'name'], true)) {
                throw new Html2PdfException("Unknown signature detail \"$key\": use reason, location, contact or name");
            }
        }
        $this->signature = [
            'cert' => $certificate,
            'key' => $privateKey,
            'reason' => (string) ($details['reason'] ?? ''),
            'location' => (string) ($details['location'] ?? ''),
            'contact' => (string) ($details['contact'] ?? ''),
            'name' => (string) ($details['name'] ?? ''),
        ];
        return $this;
    }

    public function hasFillableForms(): bool
    {
        return $this->fillableForms;
    }

    /** @return array{cert: string, key: string, reason: string, location: string, contact: string, name: string}|null */
    public function getSignature(): ?array
    {
        return $this->signature;
    }

    public function getEncryption(): string
    {
        return $this->encryption;
    }

    public function isPdfA(): bool
    {
        return $this->pdfA;
    }

    /** Seconds a conversion may take before it is stopped. */
    public function setTimeout(int $seconds): self
    {
        if ($seconds < 1) {
            throw new Html2PdfException('The timeout must be at least 1 second');
        }
        $this->timeout = $seconds;
        return $this;
    }

    public function getPaperSize(): string
    {
        return $this->paperSize;
    }

    public function getOrientation(): string
    {
        return $this->orientation;
    }

    public function getMarginMm(): float
    {
        return $this->marginMm;
    }

    public function hasPageNumbers(): bool
    {
        return $this->pageNumbers;
    }

    public function getViewportWidth(): ?int
    {
        return $this->viewportWidth;
    }

    /** @return string[] */
    public function getCssLibraries(): array
    {
        return $this->cssLibraries;
    }

    public function isRemoteEnabled(): bool
    {
        return $this->remoteEnabled;
    }

    public function getBasePath(): ?string
    {
        return $this->basePath;
    }

    public function getUserPassword(): ?string
    {
        return $this->userPassword;
    }

    public function getOwnerPassword(): ?string
    {
        return $this->ownerPassword;
    }

    /** @return array{print: bool, copy: bool, modify: bool} */
    public function getPermissions(): array
    {
        return ['print' => $this->allowPrint, 'copy' => $this->allowCopy, 'modify' => $this->allowModify];
    }

    public function getTimeout(): int
    {
        return $this->timeout;
    }

    /**
     * Command-line arguments for the converter binary. Passwords are not
     * included: they are passed in the environment (see passwordEnv()).
     *
     * @return string[]
     */
    public function toArguments(): array
    {
        $this->assertConsistent();
        $args = [
            '-size', $this->paperSize,
            '-margin', self::number($this->marginMm),
            '-css', $this->cssLibraries ? implode(',', $this->cssLibraries) : 'auto',
            '-encryption', $this->encryption,
        ];
        if ($this->orientation === self::LANDSCAPE) {
            $args[] = '-landscape';
        }
        if ($this->pageNumbers) {
            $args[] = '-numbers';
        }
        if ($this->viewportWidth !== null) {
            array_push($args, '-viewport', (string) $this->viewportWidth);
        }
        if ($this->remoteEnabled) {
            $args[] = '-remote';
        }
        if (!$this->allowPrint) {
            $args[] = '-no-print';
        }
        if (!$this->allowCopy) {
            $args[] = '-no-copy';
        }
        if (!$this->allowModify) {
            $args[] = '-no-modify';
        }
        if ($this->pdfA) {
            $args[] = '-pdfa';
        }
        if ($this->fillableForms) {
            $args[] = '-forms';
        }
        if ($this->signature !== null) {
            if ($this->signature['cert'] === '') {
                throw new Html2PdfException('Signing with the converter binary needs the certificate and key files');
            }
            array_push($args, '-sign-cert', $this->signature['cert'], '-sign-key', $this->signature['key']);
            foreach (['reason', 'location', 'contact', 'name'] as $detail) {
                if ($this->signature[$detail] !== '') {
                    array_push($args, '-sign-' . $detail, $this->signature[$detail]);
                }
            }
        }
        return $args;
    }

    /** Report option conflicts before converting. */
    private function assertConsistent(): void
    {
        $protected = $this->userPassword !== null || $this->ownerPassword !== null
            || !$this->allowPrint || !$this->allowCopy || !$this->allowModify;
        if ($this->pdfA && $protected) {
            throw new Html2PdfException('PDF/A documents cannot be encrypted: remove the password and permissions, or turn off PDF/A');
        }
        if ($this->pdfA && $this->fillableForms) {
            throw new Html2PdfException('Fillable forms cannot be combined with PDF/A: turn one of them off');
        }
    }

    /** @return array<string, string> environment variables with the passwords */
    public function passwordEnv(): array
    {
        $env = [];
        if ($this->userPassword !== null) {
            $env['HTML2PDF_USER_PASSWORD'] = $this->userPassword;
        }
        if ($this->ownerPassword !== null) {
            $env['HTML2PDF_OWNER_PASSWORD'] = $this->ownerPassword;
        }
        return $env;
    }

    /**
     * Form fields for the converter's HTTP API. cssLib is a list: the
     * field is repeated once per library.
     *
     * @return array<string, string|string[]>
     */
    public function toFormFields(): array
    {
        $this->assertConsistent();
        return [
            'pageSize' => $this->paperSize,
            'orientation' => $this->orientation,
            'margin' => self::number($this->marginMm),
            'pageNumbers' => $this->pageNumbers ? 'true' : 'false',
            'viewport' => $this->viewportWidth === null ? '' : (string) $this->viewportWidth,
            'cssLib' => $this->cssLibraries,
            'allowRemote' => $this->remoteEnabled ? 'true' : 'false',
            'userPassword' => (string) $this->userPassword,
            'ownerPassword' => (string) $this->ownerPassword,
            'denyPrint' => $this->allowPrint ? 'false' : 'true',
            'denyCopy' => $this->allowCopy ? 'false' : 'true',
            'denyModify' => $this->allowModify ? 'false' : 'true',
            'encryption' => $this->encryption,
            'pdfa' => $this->pdfA ? 'true' : 'false',
            'forms' => $this->fillableForms ? 'true' : 'false',
            'sign' => $this->signature !== null ? 'true' : 'false',
            'signReason' => $this->signature['reason'] ?? '',
            'signLocation' => $this->signature['location'] ?? '',
            'signContact' => $this->signature['contact'] ?? '',
        ];
    }

    /** @param mixed $mm a value from a custom paper size, checked here */
    private static function millimetres($mm): string
    {
        if (!is_numeric($mm) || $mm < 10 || $mm > 9999) {
            throw new Html2PdfException('Custom paper sides must be 10–9999 mm');
        }
        return self::number((float) $mm);
    }

    private static function number(float $n): string
    {
        return rtrim(rtrim(sprintf('%.2F', $n), '0'), '.');
    }
}
