<?php

declare(strict_types=1);

namespace Html2Pdf\Transport;

use Html2Pdf\Options;
use Html2Pdf\Result;

/**
 * Runs a conversion: with the bundled binary (BinaryTransport, the
 * default) or through a converter server (HttpTransport).
 */
interface TransportInterface
{
    /**
     * @param string $html         the document
     * @param string $documentName the document's path inside the base path
     *                             ("invoices/2024.html"), for relative links
     */
    public function convert(string $html, string $documentName, Options $options): Result;
}
