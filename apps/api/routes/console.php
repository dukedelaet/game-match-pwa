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
})->everyMinute();

Artisan::command('inspire', function () {
    $this->comment(Inspiring::quote());
})->purpose('Display an inspiring quote');
