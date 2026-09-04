<?php

namespace App\Console\Commands;

use Illuminate\Console\Command;
use Illuminate\Support\Facades\Cache;

class GamematchOtp extends Command
{
    protected $signature = 'gamematch:otp {phone}';

    protected $description = 'Print the cached OTP for a phone (closed beta; APP_DEBUG stays false)';

    public function handle(): int
    {
        $phone = preg_replace('/\s+/', '', (string) $this->argument('phone'));
        $code = Cache::get('otp:'.$phone);
        if (! $code) {
            $this->error('No OTP for that phone');

            return self::FAILURE;
        }
        $this->line((string) $code);

        return self::SUCCESS;
    }
}
