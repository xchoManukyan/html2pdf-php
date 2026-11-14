<?php

declare(strict_types=1);

namespace Html2Pdf\Exception;

/** The conversion took longer than Options::setTimeout() allows. */
class TimeoutException extends Html2PdfException
{
}
