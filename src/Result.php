<?php

declare(strict_types=1);

namespace Html2Pdf;

use Html2Pdf\Exception\Html2PdfException;

/**
 * A converted document: the PDF and what happened while making it.
 */
final class Result
{
    /** @var string */
    private $pdf;
    /** @var array<int, array{lib: string, source: string}> */
    private $cssLibraries;
    /** @var array<int, array{ref: string, reason: string}> */
    private $failures;
    /** @var int */
    private $renderTimeMs;

    /**
     * @param array<int, array{lib: string, source: string}> $cssLibraries
     * @param array<int, array{ref: string, reason: string}> $failures
     */
    public function __construct(string $pdf, array $cssLibraries = [], array $failures = [], int $renderTimeMs = 0)
    {
        $this->pdf = $pdf;
        $this->cssLibraries = $cssLibraries;
        $this->failures = $failures;
        $this->renderTimeMs = $renderTimeMs;
    }

    /** The PDF file contents. */
    public function getPdf(): string
    {
        return $this->pdf;
    }

    /**
     * CSS libraries applied, and how each was chosen: "selected" (in the
     * options), "script" (Tailwind's script), "link" (a Bootstrap link) or
     * "classes" (recognised by class names).
     *
     * @return array<int, array{lib: string, source: string}>
     */
    public function getCssLibraries(): array
    {
        return $this->cssLibraries;
    }

    /**
     * Images, stylesheets and fonts the document refers to that could not
     * be loaded, with the reason ("resource not found", "remote loading is
     * off", "HTTP 404" ...).
     *
     * @return array<int, array{ref: string, reason: string}>
     */
    public function getFailures(): array
    {
        return $this->failures;
    }

    /** @return string[] the references of getFailures() */
    public function getMissingResources(): array
    {
        return array_map(static function (array $f): string {
            return $f['ref'];
        }, $this->failures);
    }

    public function hasMissingResources(): bool
    {
        return $this->failures !== [];
    }

    /** Milliseconds the converter spent on the document. */
    public function getRenderTimeMs(): int
    {
        return $this->renderTimeMs;
    }

    public function save(string $path): void
    {
        if (@file_put_contents($path, $this->pdf) !== strlen($this->pdf)) {
            throw new Html2PdfException("Could not write the PDF to \"$path\"");
        }
    }
}
