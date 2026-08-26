<?php

require __DIR__.'/../vendor/autoload.php';
$app = require __DIR__.'/../bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

$s = App\Models\GameSession::latest('created_at')->first();
$a = App\Models\User::where('name', 'Alex')->first();
$j = App\Models\User::where('name', 'Jordan')->first();
App\Support\GameEngine::join($s, $a);
App\Support\GameEngine::join($s, $j);
$s->started_at = now()->subSecond();
$s->save();
App\Support\GameEngine::advance($s->fresh(['rounds.answers', 'participants']));
$s = $s->fresh(['rounds.answers', 'participants']);
echo "state={$s->state} round={$s->current_round}\n";
App\Support\GameEngine::answer($s, $a, ['choice' => 'left']);
App\Support\GameEngine::answer($s->fresh(), $j, ['choice' => 'left']);
$v = App\Support\GameEngine::view($s->fresh(), $a);
echo json_encode(['state' => $v['state'], 'reveal' => $v['reveal']], JSON_PRETTY_PRINT), "\n";
