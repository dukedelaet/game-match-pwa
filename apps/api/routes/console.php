<?php

use App\Models\GameSession;
use App\Support\GameEngine;
use Illuminate\Foundation\Inspiring;
use Illuminate\Support\Facades\Artisan;
use Illuminate\Support\Facades\Schedule;

Schedule::call(function () {
    GameSession::query()
        ->whereNotIn('state', ['completed', 'forfeit', 'cancelled'])
        ->each(fn ($s) => GameEngine::advance($s->fresh(['rounds.answers', 'participants'])));
})->everyMinute()->name('gamematch-advance')->withoutOverlapping();

Schedule::command('queue:work --stop-when-empty --max-time=50 --tries=3')
    ->everyMinute()
    ->name('gamematch-queue-drain')
    ->withoutOverlapping();

Artisan::command('inspire', function () {
    $this->comment(Inspiring::quote());
})->purpose('Display an inspiring quote');
