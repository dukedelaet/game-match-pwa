<?php

namespace App\Support;

use App\Models\FeatureFlag;

class Flags
{
    public static function get(string $key, mixed $default = null): mixed
    {
        $row = FeatureFlag::find($key);

        return $row?->value['v'] ?? $default;
    }

    public static function on(string $key): bool
    {
        return (bool) self::get($key, false);
    }
}
